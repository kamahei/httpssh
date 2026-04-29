const int kHorizontalScrollCols = 120;

bool isPowerShellShell(String shell) {
  final normalized = shell.replaceAll('\\', '/').toLowerCase();
  final executable = normalized.split('/').last;
  final name = executable.endsWith('.exe')
      ? executable.substring(0, executable.length - 4)
      : executable;
  return name == 'pwsh' || name == 'powershell';
}

int remoteColsFor({
  required String shell,
  required bool lineWrap,
  required int visibleCols,
}) {
  final cols = visibleCols.clamp(1, 500).toInt();
  // PowerShell formats and truncates some output at the console width.
  // Keep its remote PTY wide enough while xterm.dart still wraps locally.
  if (lineWrap && isPowerShellShell(shell)) {
    return cols < kHorizontalScrollCols ? kHorizontalScrollCols : cols;
  }
  return cols;
}
