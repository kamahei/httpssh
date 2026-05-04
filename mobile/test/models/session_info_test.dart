import 'package:flutter_test/flutter_test.dart';
import 'package:httpssh_mobile/models/session_info.dart';

void main() {
  group('SessionInfo.fromJson', () {
    test('hostAttached defaults to false when omitted', () {
      final info = SessionInfo.fromJson({
        'id': 'abc',
        'title': 'pwsh',
        'shell': 'pwsh',
        'cols': 120,
        'rows': 40,
        'createdAt': '2026-04-29T14:01:02Z',
        'lastIo': '2026-04-29T14:05:11Z',
        'subscribers': 1,
      });
      expect(info.hostAttached, isFalse);
    });

    test('hostAttached parses true', () {
      final info = SessionInfo.fromJson({
        'id': 'abc',
        'title': 'pwsh',
        'shell': 'pwsh',
        'cols': 120,
        'rows': 40,
        'createdAt': '2026-04-29T14:01:02Z',
        'lastIo': '2026-04-29T14:05:11Z',
        'subscribers': 2,
        'hostAttached': true,
      });
      expect(info.hostAttached, isTrue);
    });

    test('hostAttached parses false explicitly', () {
      final info = SessionInfo.fromJson({
        'id': 'abc',
        'title': 'pwsh',
        'shell': 'pwsh',
        'cols': 120,
        'rows': 40,
        'createdAt': '2026-04-29T14:01:02Z',
        'lastIo': '2026-04-29T14:05:11Z',
        'subscribers': 1,
        'hostAttached': false,
      });
      expect(info.hostAttached, isFalse);
    });
  });
}
