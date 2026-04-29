import 'package:flutter/material.dart';
import 'package:xterm/xterm.dart';

/// Built-in terminal palette presets. Names are stable identifiers used
/// as the persisted preference value.
enum TerminalPaletteId {
  defaultDark,
  solarizedDark,
  solarizedLight,
  monokai,
}

class TerminalPalettePreset {
  const TerminalPalettePreset({
    required this.id,
    required this.label,
    required this.theme,
  });
  final TerminalPaletteId id;
  final String label;
  final TerminalTheme theme;
}

/// Curated set of ANSI palettes the user can pick from.
final List<TerminalPalettePreset> terminalPalettes = [
  const TerminalPalettePreset(
    id: TerminalPaletteId.defaultDark,
    label: 'Default Dark',
    theme: TerminalTheme(
      cursor: Color(0xFFE6E6E6),
      selection: Color(0x66FFFFFF),
      foreground: Color(0xFFE6E6E6),
      background: Color(0xFF101012),
      black: Color(0xFF000000),
      red: Color(0xFFCD3131),
      green: Color(0xFF0DBC79),
      yellow: Color(0xFFE5E510),
      blue: Color(0xFF2472C8),
      magenta: Color(0xFFBC3FBC),
      cyan: Color(0xFF11A8CD),
      white: Color(0xFFE5E5E5),
      brightBlack: Color(0xFF666666),
      brightRed: Color(0xFFF14C4C),
      brightGreen: Color(0xFF23D18B),
      brightYellow: Color(0xFFF5F543),
      brightBlue: Color(0xFF3B8EEA),
      brightMagenta: Color(0xFFD670D6),
      brightCyan: Color(0xFF29B8DB),
      brightWhite: Color(0xFFE5E5E5),
      searchHitBackground: Color(0x66FFFFFF),
      searchHitBackgroundCurrent: Color(0x99FFFFFF),
      searchHitForeground: Color(0xFF000000),
    ),
  ),
  const TerminalPalettePreset(
    id: TerminalPaletteId.solarizedDark,
    label: 'Solarized Dark',
    theme: TerminalTheme(
      cursor: Color(0xFF93A1A1),
      selection: Color(0x66839496),
      foreground: Color(0xFF839496),
      background: Color(0xFF002B36),
      black: Color(0xFF073642),
      red: Color(0xFFDC322F),
      green: Color(0xFF859900),
      yellow: Color(0xFFB58900),
      blue: Color(0xFF268BD2),
      magenta: Color(0xFFD33682),
      cyan: Color(0xFF2AA198),
      white: Color(0xFFEEE8D5),
      brightBlack: Color(0xFF002B36),
      brightRed: Color(0xFFCB4B16),
      brightGreen: Color(0xFF586E75),
      brightYellow: Color(0xFF657B83),
      brightBlue: Color(0xFF839496),
      brightMagenta: Color(0xFF6C71C4),
      brightCyan: Color(0xFF93A1A1),
      brightWhite: Color(0xFFFDF6E3),
      searchHitBackground: Color(0x66839496),
      searchHitBackgroundCurrent: Color(0x99839496),
      searchHitForeground: Color(0xFF002B36),
    ),
  ),
  const TerminalPalettePreset(
    id: TerminalPaletteId.solarizedLight,
    label: 'Solarized Light',
    theme: TerminalTheme(
      cursor: Color(0xFF586E75),
      selection: Color(0x66657B83),
      foreground: Color(0xFF657B83),
      background: Color(0xFFFDF6E3),
      black: Color(0xFF073642),
      red: Color(0xFFDC322F),
      green: Color(0xFF859900),
      yellow: Color(0xFFB58900),
      blue: Color(0xFF268BD2),
      magenta: Color(0xFFD33682),
      cyan: Color(0xFF2AA198),
      white: Color(0xFFEEE8D5),
      brightBlack: Color(0xFF002B36),
      brightRed: Color(0xFFCB4B16),
      brightGreen: Color(0xFF586E75),
      brightYellow: Color(0xFF657B83),
      brightBlue: Color(0xFF839496),
      brightMagenta: Color(0xFF6C71C4),
      brightCyan: Color(0xFF93A1A1),
      brightWhite: Color(0xFFFDF6E3),
      searchHitBackground: Color(0x66657B83),
      searchHitBackgroundCurrent: Color(0x99657B83),
      searchHitForeground: Color(0xFFFDF6E3),
    ),
  ),
  const TerminalPalettePreset(
    id: TerminalPaletteId.monokai,
    label: 'Monokai',
    theme: TerminalTheme(
      cursor: Color(0xFFF8F8F0),
      selection: Color(0x6649483E),
      foreground: Color(0xFFF8F8F2),
      background: Color(0xFF272822),
      black: Color(0xFF272822),
      red: Color(0xFFF92672),
      green: Color(0xFFA6E22E),
      yellow: Color(0xFFF4BF75),
      blue: Color(0xFF66D9EF),
      magenta: Color(0xFFAE81FF),
      cyan: Color(0xFFA1EFE4),
      white: Color(0xFFF8F8F2),
      brightBlack: Color(0xFF75715E),
      brightRed: Color(0xFFF92672),
      brightGreen: Color(0xFFA6E22E),
      brightYellow: Color(0xFFF4BF75),
      brightBlue: Color(0xFF66D9EF),
      brightMagenta: Color(0xFFAE81FF),
      brightCyan: Color(0xFFA1EFE4),
      brightWhite: Color(0xFFF9F8F5),
      searchHitBackground: Color(0x66F8F8F2),
      searchHitBackgroundCurrent: Color(0x99F8F8F2),
      searchHitForeground: Color(0xFF272822),
    ),
  ),
];

TerminalPalettePreset paletteById(TerminalPaletteId id) {
  return terminalPalettes.firstWhere(
    (p) => p.id == id,
    orElse: () => terminalPalettes.first,
  );
}
