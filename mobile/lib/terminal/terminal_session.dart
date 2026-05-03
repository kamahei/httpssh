import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
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
  })  : terminal = Terminal(maxLines: 10000),
        _lineWrap = lineWrap,
        _fixedCols = fixedCols {
    terminal.onOutput = _sendInput;
    terminal.onResize = (cols, rows, _, __) => _sendResize(cols, rows);
    _connect();
  }

  final ApiClient api;
  final SessionInfo session;
  final Terminal terminal;
  bool _lineWrap;
  int _fixedCols;

  WebSocketChannel? _channel;
  Timer? _retryTimer;
  Timer? _pingTimer;
  int _retryAttempt = 0;
  bool _disposed = false;

  TerminalConnectionState _state = TerminalConnectionState.connecting;
  TerminalConnectionState get state => _state;

  String? _lastError;
  String? get lastError => _lastError;

  void updateLineWrapMode(bool lineWrap) {
    if (_lineWrap == lineWrap) return;
    _lineWrap = lineWrap;
    _sendResize(terminal.viewWidth, terminal.viewHeight);
  }

  void updateColumnWidth(int fixedCols) {
    if (_fixedCols == fixedCols) return;
    _fixedCols = fixedCols;
    _sendResize(terminal.viewWidth, terminal.viewHeight);
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
    _channel!.stream.listen(
      _onMessage,
      onError: (Object e) => _scheduleReconnect('error: $e'),
      onDone: () {
        if (_state == TerminalConnectionState.closed) return;
        _scheduleReconnect('socket closed');
      },
      cancelOnError: true,
    );
    _retryAttempt = 0;
    _setState(TerminalConnectionState.live);

    // 20 s heartbeat keeps Cloudflare and home routers from timing out.
    _pingTimer?.cancel();
    _pingTimer = Timer.periodic(const Duration(seconds: 20), (_) {
      _send({'t': 'ping'});
    });

    // Send an initial resize matching the current xterm dimensions.
    Future.microtask(() {
      _sendResize(terminal.viewWidth, terminal.viewHeight);
    });
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
        terminal.buffer.clear();
        terminal.buffer.setCursor(0, 0);
        terminal.write((frame['d'] as String?) ?? '');
      case 'out':
        terminal.write((frame['d'] as String?) ?? '');
      case 'exit':
        final code = frame['code'];
        terminal.write('\r\n[process exited code=$code]\r\n');
        _setState(TerminalConnectionState.closed);
      case 'pong':
        // no-op
        break;
      case 'error':
        final msg = (frame['message'] as String?) ?? '';
        _setState(_state, msg);
    }
  }

  void _sendInput(String data) {
    _send({'t': 'in', 'd': data});
  }

  void _sendResize(int cols, int rows) {
    _send({
      't': 'resize',
      'c': remoteColsFor(
        shell: session.shell,
        lineWrap: _lineWrap,
        visibleCols: cols,
        fixedCols: _fixedCols,
      ),
      'r': rows,
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
