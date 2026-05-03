import 'dart:convert';

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

/// Terminal column count used as
///   * the fixed width when [LineWrapNotifier] is set to scroll mode, and
///   * the minimum remote PTY width when wrap mode is paired with a
///     PowerShell shell (PSReadLine caches console width at startup, so a
///     too-narrow ConPTY can permanently truncate formatted output).
class TerminalColumnsNotifier extends AsyncNotifier<int> {
  static const _key = 'settings.terminalColumns';
  static const int defaultColumns = 120;
  static const int minColumns = 60;
  static const int maxColumns = 240;
  static const int step = 10;

  @override
  Future<int> build() async {
    final prefs = await SharedPreferences.getInstance();
    return _normalize(prefs.getInt(_key) ?? defaultColumns);
  }

  Future<void> set(int cols) async {
    final value = _normalize(cols);
    final prefs = await SharedPreferences.getInstance();
    await prefs.setInt(_key, value);
    state = AsyncData(value);
  }

  static int _normalize(int v) => v.clamp(minColumns, maxColumns).toInt();
}

final terminalColumnsProvider =
    AsyncNotifierProvider<TerminalColumnsNotifier, int>(
  TerminalColumnsNotifier.new,
);

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

/// Per-session override of [LineWrapNotifier]. A session ID present in the
/// map uses its associated value (true = wrap, false = horizontal scroll)
/// regardless of the global default; absent IDs follow the global setting.
/// Persisted as a JSON-encoded `Map<String, bool>` so the choice survives
/// app restarts and re-attaching to a long-running relay session.
class SessionLineWrapOverridesNotifier
    extends AsyncNotifier<Map<String, bool>> {
  static const _key = 'settings.sessionLineWrapOverrides';

  @override
  Future<Map<String, bool>> build() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_key);
    if (raw == null || raw.isEmpty) return <String, bool>{};
    try {
      final decoded = jsonDecode(raw);
      if (decoded is Map) {
        final out = <String, bool>{};
        decoded.forEach((k, v) {
          if (k is String && v is bool) out[k] = v;
        });
        return out;
      }
    } catch (_) {
      // Corrupted entry — drop it silently.
    }
    return <String, bool>{};
  }

  Future<void> _persist(Map<String, bool> map) async {
    final prefs = await SharedPreferences.getInstance();
    if (map.isEmpty) {
      await prefs.remove(_key);
    } else {
      await prefs.setString(_key, jsonEncode(map));
    }
    state = AsyncData(Map<String, bool>.unmodifiable(map));
  }

  /// Set or clear the override for [sessionId]. Pass `null` to fall back
  /// to the global [lineWrapProvider].
  Future<void> setOverride(String sessionId, bool? value) async {
    // Wait for the initial SharedPreferences load to settle. Without this,
    // a write that races the still-pending build() can be silently
    // overwritten when build() finally completes.
    final current = await future;
    final next = Map<String, bool>.from(current);
    if (value == null) {
      next.remove(sessionId);
    } else {
      next[sessionId] = value;
    }
    await _persist(next);
  }

  /// Drop overrides for any session ID not present in [activeSessionIds].
  /// Called after the sessions list refresh so killed sessions don't leak
  /// entries into SharedPreferences.
  Future<void> pruneTo(Set<String> activeSessionIds) async {
    final current = await future;
    if (current.isEmpty) return;
    final next = <String, bool>{};
    var changed = false;
    current.forEach((k, v) {
      if (activeSessionIds.contains(k)) {
        next[k] = v;
      } else {
        changed = true;
      }
    });
    if (changed) await _persist(next);
  }
}

final sessionLineWrapOverridesProvider =
    AsyncNotifierProvider<SessionLineWrapOverridesNotifier, Map<String, bool>>(
  SessionLineWrapOverridesNotifier.new,
);
