import 'dart:async';
import 'dart:convert';

import 'package:flutter/widgets.dart';
import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:xterm/xterm.dart';

import '../api/api_client.dart';
import '../models/profile.dart';
import '../models/session_info.dart';
import 'resize_policy.dart';

/// Visible per-tab connection lifecycle.
enum TerminalConnectionState { connecting, live, reconnecting, closed }

/// One terminal tab: owns a Terminal widget, a WebSocket, and the
/// reconnect state machine. Listenable so the surrounding widgets can
/// rebuild on state changes.
class TerminalSession extends ChangeNotifier {
  TerminalSession({
    required this.api,
    required this.session,
    required bool lineWrap,
    required int fixedCols,
  })  : _lineWrap = lineWrap,
        _fixedCols = fixedCols {
    _terminal = _newTerminal();
    _connect();
  }

  final ApiClient api;
  final SessionInfo session;
  bool _lineWrap;
  int _fixedCols;
  late Terminal _terminal;
  int _terminalGeneration = 0;

  Terminal get terminal => _terminal;

  int get terminalGeneration => _terminalGeneration;

  WebSocketChannel? _channel;
  Timer? _retryTimer;
  Timer? _pingTimer;
  int _retryAttempt = 0;
  bool _disposed = false;
  bool _connectionReady = false;
  bool _receivedReplay = false;
  String? _pendingReplay;
  final List<String> _pendingOut = <String>[];
  // Raw replay/out chunks used to rebuild xterm when presentation-only
  // settings change. xterm stores processed cells, so resizing alone cannot
  // make old scrollback follow a new wrap/scroll mode.
  final List<String> _renderHistory = <String>[];
  int _renderHistoryBytes = 0;
  int? _lastSentCols;
  int? _lastSentRows;

  static const int _renderHistoryLimitBytes = 4 * 1024 * 1024;

  /// Bumped every time a new WebSocket is opened. Stream callbacks captured
  /// against an older generation must short-circuit so an intentional
  /// reconnect (e.g. line-wrap mode change) does not trigger a redundant
  /// schedule from the stale listener's onDone.
  int _channelGen = 0;

  TerminalConnectionState _state = TerminalConnectionState.connecting;
  TerminalConnectionState get state => _state;

  String? _lastError;
  String? get lastError => _lastError;

  Terminal _newTerminal({int? cols, int? rows}) {
    final next = Terminal(maxLines: 10000, reflowEnabled: false);
    if (cols != null && rows != null) {
      next.resize(cols, rows);
    }
    next.onOutput = _sendInput;
    next.onResize = (cols, rows, _, __) => _sendResize(cols, rows);
    return next;
  }

  void updateLineWrapMode(bool lineWrap) {
    if (_lineWrap == lineWrap) return;
    _lineWrap = lineWrap;
    _rebuildTerminalFromHistory();
  }

  void updateColumnWidth(int fixedCols) {
    if (_fixedCols == fixedCols) return;
    _fixedCols = fixedCols;
    if (_lineWrap) {
      _sendResize(terminal.viewWidth, terminal.viewHeight);
      return;
    }
    _rebuildTerminalFromHistory();
  }

  void _setState(TerminalConnectionState s, [String? err]) {
    _state = s;
    _lastError = err;
    notifyListeners();
  }

  Map<String, dynamic> _wsHeaders() {
    final p = api.profile;
    final h = <String, dynamic>{};
    switch (p.authMode) {
      case AuthMode.bearerOnly:
        break;
      case AuthMode.bearerPlusServiceToken:
        if (p.cfClientId != null) h['CF-Access-Client-Id'] = p.cfClientId!;
        if (p.cfClientSecret != null) {
          h['CF-Access-Client-Secret'] = p.cfClientSecret!;
        }
      case AuthMode.bearerPlusBrowserSso:
        if (p.sessionCookie != null && p.sessionCookie!.isNotEmpty) {
          h['Cookie'] = p.sessionCookie!;
        }
    }
    return h;
  }

  void _connect() {
    if (_disposed) return;
    _setState(TerminalConnectionState.connecting);
    final url = api.wsUrlForSession(session.id);
    try {
      // The LAN bearer travels in the URL via ?token=. The CF edge
      // layer (Service Token headers or SSO cookie) cannot ride in the
      // query string, so for the dart:io WebSocket we attach it as
      // a request header on the upgrade.
      _channel = IOWebSocketChannel.connect(
        url,
        protocols: const ['httpssh.v1'],
        headers: _wsHeaders(),
      );
    } catch (e) {
      _scheduleReconnect('connect failed: $e');
      return;
    }
    final myGen = ++_channelGen;
    _connectionReady = false;
    _receivedReplay = false;
    _pendingReplay = null;
    _pendingOut.clear();
    _channel!.stream.listen(
      (raw) {
        // After an auto-reconnect (network blip recovered via
        // _scheduleReconnect), the old channel can still deliver
        // buffered frames to the dart Stream listener even though we
        // have closed our end. Without this guard those frames get
        // written into the terminal AFTER the new connection's replay
        // has already populated it, producing visible duplicates of the
        // most recent output.
        if (myGen != _channelGen) return;
        _onMessage(raw);
      },
      onError: (Object e) {
        if (myGen != _channelGen) return;
        _scheduleReconnect('error: $e');
      },
      onDone: () {
        if (myGen != _channelGen) return;
        if (_state == TerminalConnectionState.closed) return;
        _scheduleReconnect('socket closed');
      },
      cancelOnError: true,
    );
    _retryAttempt = 0;
  }

  void _onMessage(dynamic raw) {
    if (_disposed) return;
    final text = raw is String
        ? raw
        : (raw is List<int> ? utf8.decode(raw) : raw.toString());
    Map<String, dynamic> frame;
    try {
      frame = jsonDecode(text) as Map<String, dynamic>;
    } catch (_) {
      return;
    }
    switch (frame['t'] as String?) {
      case 'replay':
        // xterm.dart does not expose a full reset equivalent to xterm.js.
        // Clearing only the buffer leaves widget-side scroll/selection/cache
        // state around, which can make full replay appear duplicated after a
        // reconnect. Replace the terminal object so the next build also
        // recreates TerminalView's internal controllers.
        //
        // Do not write the replay immediately. Reconnects briefly show a
        // status banner, and switching from connecting -> live changes the
        // terminal body's height. If replay is written before that layout
        // settles, xterm's resize path can visually reflow the freshly written
        // scrollback. Queue it until the remounted TerminalView has completed
        // its first layout pass at the live size.
        final old = terminal;
        old.onOutput = null;
        old.onResize = null;
        final next = _newTerminal(cols: old.viewWidth, rows: old.viewHeight);
        _terminal = next;
        _terminalGeneration++;
        _receivedReplay = true;
        final replay = (frame['d'] as String?) ?? '';
        _resetRenderHistory(replay);
        _pendingReplay = replay;
        _pendingOut.clear();
        _connectionReady = false;
        final replayGeneration = _terminalGeneration;
        _setState(TerminalConnectionState.live);
        _schedulePendingReplayApply(replayGeneration);
      case 'out':
        if (!_receivedReplay) return;
        final data = (frame['d'] as String?) ?? '';
        _appendRenderHistory(data);
        if (!_connectionReady) {
          _pendingOut.add(data);
          return;
        }
        terminal.write(data);
      case 'exit':
        final code = frame['code'];
        final message = '\r\n[process exited code=$code]\r\n';
        _appendRenderHistory(message);
        if (_receivedReplay && !_connectionReady) {
          _pendingOut.add(message);
        } else {
          terminal.write(message);
        }
        _connectionReady = false;
        _setState(TerminalConnectionState.closed);
      case 'pong':
        // no-op
        break;
      case 'error':
        final msg = (frame['message'] as String?) ?? '';
        _setState(_state, msg);
    }
  }

  void _schedulePendingReplayApply(int replayGeneration) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        _applyPendingReplay(replayGeneration);
      });
    });
  }

  void _applyPendingReplay(int replayGeneration) {
    if (_disposed || replayGeneration != _terminalGeneration) return;
    final replay = _pendingReplay;
    if (replay == null) return;
    _pendingReplay = null;
    terminal.write(replay);
    for (final data in _pendingOut) {
      terminal.write(data);
    }
    _pendingOut.clear();
    if (_state != TerminalConnectionState.closed) {
      _connectionReady = true;
      _startPing();
      _sendResize(terminal.viewWidth, terminal.viewHeight);
    }
  }

  void _rebuildTerminalFromHistory() {
    if (!_receivedReplay) {
      _sendResize(terminal.viewWidth, terminal.viewHeight);
      return;
    }
    final replay = _renderHistory.join();
    final old = terminal;
    old.onOutput = null;
    old.onResize = null;
    final next = _newTerminal(cols: old.viewWidth, rows: old.viewHeight);
    _terminal = next;
    _terminalGeneration++;
    _pendingReplay = replay;
    _pendingOut.clear();
    _connectionReady = false;
    _lastSentCols = null;
    _lastSentRows = null;
    final replayGeneration = _terminalGeneration;
    _schedulePendingReplayApply(replayGeneration);
  }

  void _resetRenderHistory(String data) {
    _renderHistory.clear();
    _renderHistoryBytes = 0;
    _appendRenderHistory(data);
  }

  void _appendRenderHistory(String data) {
    if (data.isEmpty) return;
    _renderHistory.add(data);
    _renderHistoryBytes += utf8.encode(data).length;
    while (_renderHistoryBytes > _renderHistoryLimitBytes &&
        _renderHistory.length > 1) {
      final removed = _renderHistory.removeAt(0);
      _renderHistoryBytes -= utf8.encode(removed).length;
    }
  }

  @visibleForTesting
  void applyPendingReplayForTesting() {
    _applyPendingReplay(_terminalGeneration);
  }

  @visibleForTesting
  int get pendingOutCountForTesting => _pendingOut.length;

  void _sendInput(String data) {
    if (!_connectionReady) return;
    _send({'t': 'in', 'd': data});
  }

  void _sendResize(int cols, int rows) {
    if (!_connectionReady) return;
    final remoteCols = remoteColsFor(
      shell: session.shell,
      lineWrap: _lineWrap,
      visibleCols: cols,
      fixedCols: _fixedCols,
    );
    if (_lastSentCols == remoteCols && _lastSentRows == rows) return;
    _lastSentCols = remoteCols;
    _lastSentRows = rows;
    _send({'t': 'resize', 'c': remoteCols, 'r': rows});
  }

  void _startPing() {
    _pingTimer?.cancel();
    _pingTimer = Timer.periodic(const Duration(seconds: 20), (_) {
      if (_connectionReady) _send({'t': 'ping'});
    });
  }

  void _send(Map<String, dynamic> frame) {
    final ch = _channel;
    if (ch == null) return;
    try {
      ch.sink.add(jsonEncode(frame));
    } catch (_) {
      // The stream listener will pick this up via onError.
    }
  }

  void _scheduleReconnect(String reason) {
    if (_disposed || _state == TerminalConnectionState.closed) return;
    _channelGen++;
    _connectionReady = false;
    _receivedReplay = false;
    _pendingReplay = null;
    _pendingOut.clear();
    _pingTimer?.cancel();
    _pingTimer = null;
    try {
      _channel?.sink.close();
    } catch (_) {}
    _channel = null;
    _setState(TerminalConnectionState.reconnecting, reason);

    const delays = [1, 2, 5, 10, 30];
    final wait = Duration(
      seconds: delays[_retryAttempt.clamp(0, delays.length - 1)],
    );
    _retryAttempt++;
    _retryTimer?.cancel();
    _retryTimer = Timer(wait, _connect);
  }

  /// Fully close the session locally. The server-side session remains
  /// alive (this is the deliberate "detach" behavior).
  void disposeChannel({bool markClosed = true}) {
    _channelGen++;
    _connectionReady = false;
    _receivedReplay = false;
    _pendingReplay = null;
    _pendingOut.clear();
    _retryTimer?.cancel();
    _pingTimer?.cancel();
    try {
      _channel?.sink.close();
    } catch (_) {}
    _channel = null;
    if (markClosed) _setState(TerminalConnectionState.closed);
  }

  @override
  void dispose() {
    _disposed = true;
    disposeChannel(markClosed: false);
    super.dispose();
  }
}
