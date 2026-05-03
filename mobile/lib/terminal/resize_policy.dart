/// Default fallback for [remoteColsFor]'s `fixedCols` argument and for
/// callers that have no access to the user's configured column width.
/// The user-configurable value lives in `state/settings.dart`
/// (`TerminalColumnsNotifier`); pass it in via `fixedCols`.
const int kHorizontalScrollCols = 120;

bool isPowerShellShell(String shell) {
  final normalized = shell.replaceAll('\\', '/').toLowerCase();
  final executable = normalized.split('/').last;
  final name = executable.endsWith('.exe')
      ? executable.substring(0, executable.length - 4)
      : executable;
  return name == 'pwsh' || name == 'powershell';
}

/// Compute the cols value to send to the relay PTY.
///
/// * In wrap mode the remote width matches [visibleCols], except when
///   the shell is PowerShell — there we keep at least [fixedCols] so
///   PSReadLine has a wide enough console to format wide output.
/// * In scroll mode the remote width is pinned to [fixedCols] regardless
///   of the visible viewport.
int remoteColsFor({
  required String shell,
  required bool lineWrap,
  required int visibleCols,
  required int fixedCols,
}) {
  final cols = visibleCols.clamp(1, 500).toInt();
  final pinned = fixedCols.clamp(1, 500).toInt();
  if (!lineWrap) return pinned;
  if (isPowerShellShell(shell)) {
    return cols < pinned ? pinned : cols;
  }
  return cols;
}
