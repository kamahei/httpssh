import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../l10n/generated/app_localizations.dart';
import '../state/settings.dart';
import '../terminal/resize_policy.dart';
import '../terminal/themes.dart'
    show terminalPalettes, paletteById, TerminalPaletteId;

class SettingsScreen extends ConsumerWidget {
  const SettingsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = AppLocalizations.of(context)!;
    final localeAsync = ref.watch(localeProvider);
    final themeAsync = ref.watch(themeModeProvider);
    final paletteAsync = ref.watch(terminalPaletteProvider);
    final fontAsync = ref.watch(terminalFontSizeProvider);
    final wrapAsync = ref.watch(lineWrapProvider);

    return Scaffold(
      appBar: AppBar(title: Text(t.settingsTitle)),
      body: ListView(
        children: [
          ListTile(
            title: Text(t.settingsLanguage),
            subtitle: localeAsync.when(
              data: (l) => Text(_localeLabel(t, l)),
              loading: () => const Text('...'),
              error: (e, _) => Text('$e'),
            ),
            onTap: () => _pickLocale(context, ref),
          ),
          ListTile(
            title: Text(t.settingsTheme),
            subtitle: themeAsync.when(
              data: (m) => Text(_themeLabel(t, m)),
              loading: () => const Text('...'),
              error: (e, _) => Text('$e'),
            ),
            onTap: () => _pickTheme(context, ref),
          ),
          ListTile(
            title: Text(t.settingsTerminalPalette),
            subtitle: paletteAsync.when(
              data: (p) => Text(paletteById(p).label),
              loading: () => const Text('...'),
              error: (e, _) => Text('$e'),
            ),
            onTap: () => _pickPalette(context, ref),
          ),
          wrapAsync.when(
            loading: () => const ListTile(title: Text('...')),
            error: (e, _) => ListTile(title: Text('$e')),
            data: (wrap) => Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
                  child: Text(
                    t.settingsLineWrap,
                    style: Theme.of(context).textTheme.bodyMedium,
                  ),
                ),
                Padding(
                  padding:
                      const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                  child: SegmentedButton<bool>(
                    segments: [
                      ButtonSegment(
                        value: true,
                        icon: const Icon(Icons.wrap_text),
                        label: Text(t.settingsLineWrapWrap),
                      ),
                      ButtonSegment(
                        value: false,
                        icon: const Icon(Icons.swap_horiz),
                        label: Text(t.settingsLineWrapScroll),
                      ),
                    ],
                    selected: {wrap},
                    onSelectionChanged: (s) =>
                        ref.read(lineWrapProvider.notifier).set(s.first),
                  ),
                ),
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
                  child: Text(
                    t.settingsLineWrapHint(kHorizontalScrollCols),
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: Theme.of(context).colorScheme.outline,
                        ),
                  ),
                ),
              ],
            ),
          ),
          fontAsync.when(
            loading: () => const ListTile(title: Text('...')),
            error: (e, _) => ListTile(title: Text('$e')),
            data: (size) => Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    t.settingsTerminalFontSize,
                    style: Theme.of(context).textTheme.bodyMedium,
                  ),
                  Row(
                    children: [
                      Expanded(
                        child: Slider(
                          value: size,
                          min: 8,
                          max: 28,
                          divisions: 20,
                          label: size.toStringAsFixed(0),
                          onChanged: (v) => ref
                              .read(terminalFontSizeProvider.notifier)
                              .set(v),
                        ),
                      ),
                      SizedBox(
                        width: 32,
                        child: Text(
                          size.toStringAsFixed(0),
                          textAlign: TextAlign.right,
                        ),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ),
          const Divider(),
          AboutListTile(
            applicationName: t.appTitle,
            applicationVersion: '0.1.0',
            child: Text(t.settingsAbout),
          ),
        ],
      ),
    );
  }

  String _localeLabel(AppLocalizations t, Locale? locale) {
    if (locale == null) return t.settingsLanguageSystem;
    return switch (locale.languageCode) {
      'en' => t.settingsLanguageEnglish,
      'ja' => t.settingsLanguageJapanese,
      _ => locale.toLanguageTag(),
    };
  }

  String _themeLabel(AppLocalizations t, ThemeMode mode) => switch (mode) {
        ThemeMode.system => t.settingsThemeSystem,
        ThemeMode.light => t.settingsThemeLight,
        ThemeMode.dark => t.settingsThemeDark,
      };

  Future<void> _pickLocale(BuildContext context, WidgetRef ref) async {
    final t = AppLocalizations.of(context)!;
    final picked = await showDialog<Locale?>(
      context: context,
      builder: (_) => SimpleDialog(
        title: Text(t.settingsLanguage),
        children: [
          SimpleDialogOption(
            child: Text(t.settingsLanguageSystem),
            onPressed: () => Navigator.pop(context, null),
          ),
          SimpleDialogOption(
            child: Text(t.settingsLanguageEnglish),
            onPressed: () => Navigator.pop(context, const Locale('en')),
          ),
          SimpleDialogOption(
            child: Text(t.settingsLanguageJapanese),
            onPressed: () => Navigator.pop(context, const Locale('ja')),
          ),
        ],
      ),
    );
    if (!context.mounted) return;
    await ref.read(localeProvider.notifier).set(picked);
  }

  Future<void> _pickTheme(BuildContext context, WidgetRef ref) async {
    final t = AppLocalizations.of(context)!;
    final picked = await showDialog<ThemeMode>(
      context: context,
      builder: (_) => SimpleDialog(
        title: Text(t.settingsTheme),
        children: [
          SimpleDialogOption(
            child: Text(t.settingsThemeSystem),
            onPressed: () => Navigator.pop(context, ThemeMode.system),
          ),
          SimpleDialogOption(
            child: Text(t.settingsThemeLight),
            onPressed: () => Navigator.pop(context, ThemeMode.light),
          ),
          SimpleDialogOption(
            child: Text(t.settingsThemeDark),
            onPressed: () => Navigator.pop(context, ThemeMode.dark),
          ),
        ],
      ),
    );
    if (picked == null || !context.mounted) return;
    await ref.read(themeModeProvider.notifier).set(picked);
  }

  Future<void> _pickPalette(BuildContext context, WidgetRef ref) async {
    final t = AppLocalizations.of(context)!;
    final picked = await showDialog<TerminalPaletteId>(
      context: context,
      builder: (_) => SimpleDialog(
        title: Text(t.settingsTerminalPalette),
        children: [
          for (final p in terminalPalettes)
            SimpleDialogOption(
              child: Text(p.label),
              onPressed: () => Navigator.pop(context, p.id),
            ),
        ],
      ),
    );
    if (picked == null || !context.mounted) return;
    await ref.read(terminalPaletteProvider.notifier).set(picked);
  }
}
