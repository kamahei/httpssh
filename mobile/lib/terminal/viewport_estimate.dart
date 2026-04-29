import 'package:flutter/material.dart';

/// Roughly estimate how many character cells fit in the area available
/// to a [TerminalView] given the current screen and the configured
/// terminal font size.
///
/// This is a rough estimate — it does not have access to the exact
/// monospace cell width that xterm.dart computes from the rendered
/// font. We use it ONLY to pre-size the relay's ConPTY at session
/// creation time. After the session is attached, xterm's
/// performLayout measures the actual cell size and emits an exact
/// resize over the WebSocket; the ConPTY follows.
///
/// Why pre-size at all? Because PowerShell + PSReadLine cache the
/// initial `[Console]::WindowWidth` they observe at startup, and may
/// not always pick up subsequent ConPTY resizes. By starting the
/// ConPTY close to the real viewport we avoid the visible
/// "PowerShell formatted for the wrong width" symptom that cmd does
/// not exhibit (cmd has no such cache).
({int cols, int rows}) estimateViewportCells(
  BuildContext context, {
  required double fontSize,
}) {
  final mq = MediaQuery.of(context);
  // Empirical cell metrics for the Flutter monospace stack at the
  // configured fontSize. xterm.dart will measure exactly later; this
  // estimate just needs to be in the right ballpark.
  final cellW = fontSize * 0.6;
  final cellH = fontSize * 1.2;

  // Subtract chrome that is likely to sit between the screen edge and
  // the terminal: top status bar, AppBar (~56), tab strip (~44),
  // soft-key bar (~44), bottom safe area, IME if up.
  final chromeH =
      mq.padding.top + 56 + 44 + 44 + mq.padding.bottom + mq.viewInsets.bottom;
  final availW = mq.size.width;
  final availH = (mq.size.height - chromeH).clamp(80.0, double.infinity);

  final cols = (availW / cellW).floor().clamp(20, 500);
  final rows = (availH / cellH).floor().clamp(5, 200);
  return (cols: cols, rows: rows);
}
