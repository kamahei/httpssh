import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../terminal/themes.dart';

/// User-selected app locale. `null` follows the OS.
class LocaleNotifier extends AsyncNotifier<Locale?> {
  static const _key = 'settings.locale';

  @override
  Future<Locale?> build() async {
    final prefs = await SharedPreferences.getInstance();
    final code = prefs.getString(_key);
    if (code == null || code.isEmpty) return null;
    return Locale(code);
  }

  Future<void> set(Locale? locale) async {
    final prefs = await SharedPreferences.getInstance();
    if (locale == null) {
      await prefs.remove(_key);
    } else {
      await prefs.setString(_key, locale.languageCode);
    }
    state = AsyncData(locale);
  }
}

final localeProvider =
    AsyncNotifierProvider<LocaleNotifier, Locale?>(LocaleNotifier.new);

/// User-selected theme mode.
class ThemeModeNotifier extends AsyncNotifier<ThemeMode> {
  static const _key = 'settings.themeMode';

  @override
  Future<ThemeMode> build() async {
    final prefs = await SharedPreferences.getInstance();
    final code = prefs.getString(_key);
    return switch (code) {
      'light' => ThemeMode.light,
      'dark' => ThemeMode.dark,
      _ => ThemeMode.system,
    };
  }

  Future<void> set(ThemeMode mode) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_key, mode.name);
    state = AsyncData(mode);
  }
}

final themeModeProvider =
    AsyncNotifierProvider<ThemeModeNotifier, ThemeMode>(ThemeModeNotifier.new);

class TerminalPaletteNotifier extends AsyncNotifier<TerminalPaletteId> {
  static const _key = 'settings.terminalPalette';

  @override
  Future<TerminalPaletteId> build() async {
    final prefs = await SharedPreferences.getInstance();
    final code = prefs.getString(_key);
    return TerminalPaletteId.values.firstWhere(
      (e) => e.name == code,
      orElse: () => TerminalPaletteId.defaultDark,
    );
  }

  Future<void> set(TerminalPaletteId id) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_key, id.name);
    state = AsyncData(id);
  }
}

final terminalPaletteProvider =
    AsyncNotifierProvider<TerminalPaletteNotifier, TerminalPaletteId>(
  TerminalPaletteNotifier.new,
);

class TerminalFontSizeNotifier extends AsyncNotifier<double> {
  static const _key = 'settings.terminalFontSize';
  static const double defaultSize = 14;

  @override
  Future<double> build() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getDouble(_key) ?? defaultSize;
  }

  Future<void> set(double size) async {
    final clamped = size.clamp(8.0, 28.0).toDouble();
    final prefs = await SharedPreferences.getInstance();
    await prefs.setDouble(_key, clamped);
    state = AsyncData(clamped);
  }
}

final terminalFontSizeProvider =
    AsyncNotifierProvider<TerminalFontSizeNotifier, double>(
  TerminalFontSizeNotifier.new,
);

/// Line-wrap mode for the terminal viewport.
///
///   true  -> The terminal width matches the visible viewport. Long
///            lines wrap at the screen edge. Default.
///   false -> The terminal width is fixed wide (default 120 columns).
///            Long lines do not wrap; the viewport scrolls horizontally
///            to reveal the rest. Recommended when working with shells
///            (notably PowerShell + PSReadLine) that cache the initial
///            console width and format wide output regardless of the
///            ConPTY's actual cols.
class LineWrapNotifier extends AsyncNotifier<bool> {
  static const _key = 'settings.lineWrap';

  @override
  Future<bool> build() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getBool(_key) ?? true;
  }

  Future<void> set(bool value) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_key, value);
    state = AsyncData(value);
  }
}

final lineWrapProvider =
    AsyncNotifierProvider<LineWrapNotifier, bool>(LineWrapNotifier.new);

/// Per-session idle reaper budget (seconds) sent on `POST /api/sessions`.
/// `0` means "unlimited" (the relay never reaps the session for idleness).
/// The default mirrors the relay's historical behavior of 24 hours so
/// existing users see no change until they touch the slider.
class SessionIdleTimeoutNotifier extends AsyncNotifier<int> {
  static const _key = 'settings.sessionIdleTimeoutSeconds';
  static const int defaultSeconds = 24 * 60 * 60;
  static const int maxSeconds = 168 * 60 * 60;

  @override
  Future<int> build() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getInt(_key);
    if (raw == null) return defaultSeconds;
    return _normalize(raw);
  }

  Future<void> set(int seconds) async {
    final value = _normalize(seconds);
    final prefs = await SharedPreferences.getInstance();
    await prefs.setInt(_key, value);
    state = AsyncData(value);
  }

  static int _normalize(int seconds) {
    if (seconds <= 0) return 0;
    if (seconds > maxSeconds) return maxSeconds;
    return seconds;
  }
}

final sessionIdleTimeoutProvider =
    AsyncNotifierProvider<SessionIdleTimeoutNotifier, int>(
  SessionIdleTimeoutNotifier.new,
);
