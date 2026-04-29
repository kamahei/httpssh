import 'package:flutter_test/flutter_test.dart';
import 'package:httpssh_mobile/files/text_search.dart';

void main() {
  group('findTextMatches', () {
    test('finds case-insensitive matches', () {
      final matches = findTextMatches('Alpha alpha', 'ALPHA');
      expect(matches, hasLength(2));
      expect(matches.first.start, 0);
      expect(matches.last.start, 6);
    });

    test('returns empty matches for empty query', () {
      expect(findTextMatches('content', ''), isEmpty);
    });
  });

  group('nextTextMatchIndex', () {
    test('wraps forward and backward', () {
      expect(nextTextMatchIndex(1, 2, forward: true), 0);
      expect(nextTextMatchIndex(0, 2, forward: false), 1);
    });

    test('starts at an edge when current is invalid', () {
      expect(nextTextMatchIndex(-1, 2, forward: true), 0);
      expect(nextTextMatchIndex(-1, 2, forward: false), 1);
    });
  });
}
