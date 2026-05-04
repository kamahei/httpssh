import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:httpssh_mobile/api/api_client.dart';
import 'package:httpssh_mobile/models/profile.dart';
import 'package:httpssh_mobile/models/session_info.dart';
import 'package:httpssh_mobile/terminal/terminal_session.dart';

void main() {
  testWidgets('buffers live output until deferred replay is applied', (
    tester,
  ) async {
    TerminalSession? session;
    HttpServer? server;
    WebSocket? socket;

    try {
      await tester.runAsync(() async {
        server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
        server!.listen((request) async {
          if (!WebSocketTransformer.isUpgradeRequest(request)) {
            request.response.statusCode = HttpStatus.notFound;
            await request.response.close();
            return;
          }

          socket = await WebSocketTransformer.upgrade(request);
          socket!.add(jsonEncode({'t': 'replay', 'd': 'replay\r\n'}));
          socket!.add(jsonEncode({'t': 'out', 'd': 'live\r\n'}));
        });

        session = TerminalSession(
          api: ApiClient(
            Profile(
              id: 'profile-1',
              name: 'Local',
              baseUrl: 'http://127.0.0.1:${server!.port}',
              authMode: AuthMode.bearerOnly,
              lanBearer: 'token',
            ),
          ),
          session: SessionInfo(
            id: 'session-1',
            title: 'PowerShell',
            shell: 'pwsh',
            cols: 120,
            rows: 24,
            createdAt: DateTime.utc(2026),
            lastIo: DateTime.utc(2026),
            subscribers: 0,
          ),
          lineWrap: true,
          fixedCols: 120,
        );

        await _waitFor(() => session!.terminalGeneration == 1);
      });

      expect(session!.terminal.buffer.getText(), isNot(contains('replay')));
      expect(session!.terminal.buffer.getText(), isNot(contains('live')));

      session!.applyPendingReplayForTesting();

      final text = session!.terminal.buffer.getText();
      expect(text, contains('replay'));
      expect(text, contains('live'));
      expect(text.indexOf('replay'), lessThan(text.indexOf('live')));
    } finally {
      await tester.runAsync(() async {
        session?.dispose();
        await socket?.close();
        await server?.close(force: true);
      });
    }
  });

  testWidgets('re-renders stored history when line wrap mode changes', (
    tester,
  ) async {
    TerminalSession? session;
    HttpServer? server;
    WebSocket? socket;

    try {
      await tester.runAsync(() async {
        server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
        server!.listen((request) async {
          if (!WebSocketTransformer.isUpgradeRequest(request)) {
            request.response.statusCode = HttpStatus.notFound;
            await request.response.close();
            return;
          }

          socket = await WebSocketTransformer.upgrade(request);
          socket!.add(jsonEncode({'t': 'replay', 'd': 'replay\r\n'}));
          socket!.add(jsonEncode({'t': 'out', 'd': 'live\r\n'}));
        });

        session = TerminalSession(
          api: ApiClient(
            Profile(
              id: 'profile-1',
              name: 'Local',
              baseUrl: 'http://127.0.0.1:${server!.port}',
              authMode: AuthMode.bearerOnly,
              lanBearer: 'token',
            ),
          ),
          session: SessionInfo(
            id: 'session-1',
            title: 'PowerShell',
            shell: 'pwsh',
            cols: 120,
            rows: 24,
            createdAt: DateTime.utc(2026),
            lastIo: DateTime.utc(2026),
            subscribers: 0,
          ),
          lineWrap: true,
          fixedCols: 120,
        );

        await _waitFor(() => session!.terminalGeneration == 1);
      });

      session!.applyPendingReplayForTesting();
      expect(session!.terminal.buffer.getText(), contains('replay'));
      expect(session!.terminal.buffer.getText(), contains('live'));

      final generationBeforeToggle = session!.terminalGeneration;
      session!.updateLineWrapMode(false);

      expect(session!.terminalGeneration, generationBeforeToggle + 1);
      expect(session!.terminal.buffer.getText(), isNot(contains('replay')));
      expect(session!.terminal.buffer.getText(), isNot(contains('live')));

      session!.applyPendingReplayForTesting();

      final text = session!.terminal.buffer.getText();
      expect(text, contains('replay'));
      expect(text, contains('live'));
      expect(text.indexOf('replay'), lessThan(text.indexOf('live')));
    } finally {
      await tester.runAsync(() async {
        session?.dispose();
        await socket?.close();
        await server?.close(force: true);
      });
    }
  });
}

Future<void> _waitFor(bool Function() condition) async {
  final deadline = DateTime.now().add(const Duration(seconds: 2));
  while (!condition()) {
    if (DateTime.now().isAfter(deadline)) {
      fail('condition was not met before timeout');
    }
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
}
