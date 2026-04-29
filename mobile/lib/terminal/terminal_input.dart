String terminalInputFromEditorText(
  String text, {
  required bool appendEnter,
}) {
  var normalized = text.replaceAll('\r\n', '\n').replaceAll('\r', '\n');
  if (appendEnter && !normalized.endsWith('\n')) {
    normalized = '$normalized\n';
  }
  return normalized.replaceAll('\n', '\r');
}
