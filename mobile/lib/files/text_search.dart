class TextMatch {
  const TextMatch({required this.start, required this.end});

  final int start;
  final int end;
}

List<TextMatch> findTextMatches(
  String content,
  String query, {
  bool caseSensitive = false,
}) {
  if (query.isEmpty) return const [];
  final haystack = caseSensitive ? content : content.toLowerCase();
  final needle = caseSensitive ? query : query.toLowerCase();
  final matches = <TextMatch>[];
  var start = 0;
  while (start <= haystack.length) {
    final index = haystack.indexOf(needle, start);
    if (index < 0) break;
    matches.add(TextMatch(start: index, end: index + needle.length));
    start = index + needle.length;
  }
  return matches;
}

int nextTextMatchIndex(
  int current,
  int count, {
  required bool forward,
}) {
  if (count == 0) return -1;
  if (current < 0 || current >= count) {
    return forward ? 0 : count - 1;
  }
  if (forward) return (current + 1) % count;
  return (current - 1 + count) % count;
}
