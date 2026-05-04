import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../l10n/generated/app_localizations.dart';
import '../models/profile.dart';
import '../models/session_info.dart';
import '../state/settings.dart';
import '../terminal/resize_policy.dart';
import '../terminal/viewport_estimate.dart';
import 'file_browser_screen.dart';
import 'terminal_workspace.dart';

class SessionsScreen extends ConsumerStatefulWidget {
  const SessionsScreen({super.key, required this.profile});
  final Profile profile;

  @override
  ConsumerState<SessionsScreen> createState() => _SessionsScreenState();
}

class _SessionsScreenState extends ConsumerState<SessionsScreen> {
  late final ApiClient _api = ApiClient(widget.profile);
  Future<List<SessionInfo>>? _future;
  Timer? _refreshTimer;

  @override
  void initState() {
    super.initState();
    _refresh();
    _refreshTimer =
        Timer.periodic(const Duration(seconds: 5), (_) => _refresh());
  }

  @override
  void dispose() {
    _refreshTimer?.cancel();
    super.dispose();
  }

  void _refresh() {
    final fut = _api.listSessions();
    setState(() {
      _future = fut;
    });
    fut.then((list) {
      if (!mounted) return;
      // Killed sessions should not leave orphan per-session line-wrap
      // overrides behind in SharedPreferences.
      ref.read(sessionLineWrapOverridesProvider.notifier).pruneTo(
            list.map((s) => s.id).toSet(),
          );
    }).catchError((_) {/* surfaced via FutureBuilder */});
  }

  Future<void> _create() async {
    final t = AppLocalizations.of(context)!;
    final shell = await showModalBottomSheet<String>(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              title: Text(t.sessionShellPwsh),
              onTap: () => Navigator.pop(ctx, 'pwsh'),
            ),
            ListTile(
              title: Text(t.sessionShellPowerShell),
              onTap: () => Navigator.pop(ctx, 'powershell'),
            ),
            ListTile(
              title: Text(t.sessionShellCmd),
              onTap: () => Navigator.pop(ctx, 'cmd'),
            ),
          ],
        ),
      ),
    );
    if (shell == null || !mounted) return;
    try {
      final fontSize = ref.read(terminalFontSizeProvider).maybeWhen(
            data: (s) => s,
            orElse: () => TerminalFontSizeNotifier.defaultSize,
          );
      final lineWrap = ref.read(lineWrapProvider).maybeWhen(
            data: (v) => v,
            orElse: () => true,
          );
      final fixedCols = ref.read(terminalColumnsProvider).maybeWhen(
            data: (v) => v,
            orElse: () => TerminalColumnsNotifier.defaultColumns,
          );
      final idleTimeout = ref.read(sessionIdleTimeoutProvider).maybeWhen(
            data: (v) => v,
            orElse: () => SessionIdleTimeoutNotifier.defaultSeconds,
          );
      final dims = estimateViewportCells(context, fontSize: fontSize);
      // Pre-size the ConPTY to the same remote width policy used after
      // attach. PowerShell can cache WindowWidth at startup, so a narrow
      // initial size can permanently truncate formatted output.
      final cols = remoteColsFor(
        shell: shell,
        lineWrap: lineWrap,
        visibleCols: dims.cols,
        fixedCols: fixedCols,
      );
      final created = await _api.createSession(
        shell: shell,
        cols: cols,
        rows: dims.rows,
        idleTimeoutSeconds: idleTimeout,
      );
      if (!mounted) return;
      _refresh();
      await Navigator.of(context).push(
        MaterialPageRoute<void>(
          builder: (_) => TerminalWorkspace(
            profile: widget.profile,
            initialSession: created,
          ),
        ),
      );
      _refresh();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('$e')),
      );
    }
  }

  Future<void> _kill(SessionInfo s) async {
    final t = AppLocalizations.of(context)!;
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        content: Text(t.sessionKillConfirm(s.title)),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(t.cancel),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t.sessionKill),
          ),
        ],
      ),
    );
    if (ok ?? false) {
      try {
        await _api.killSession(s.id);
      } catch (_) {/* ignore – list refresh shows reality */}
      _refresh();
    }
  }

  Future<void> _openFiles() async {
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => FileBrowserScreen(profile: widget.profile),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context)!;
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.profile.name),
        actions: [
          IconButton(
            icon: const Icon(Icons.folder_outlined),
            tooltip: t.filesOpenFileBrowser,
            onPressed: _openFiles,
          ),
          IconButton(
            icon: const Icon(Icons.refresh),
            tooltip: t.sessionsRefresh,
            onPressed: _refresh,
          ),
        ],
      ),
      body: FutureBuilder<List<SessionInfo>>(
        future: _future,
        builder: (ctx, snap) {
          if (snap.connectionState == ConnectionState.waiting &&
              !snap.hasData) {
            return const Center(child: CircularProgressIndicator());
          }
          if (snap.hasError) {
            return Center(child: Text('${snap.error}'));
          }
          final raw = snap.data ?? const <SessionInfo>[];
          // Surface host-attached sessions first so users instantly see
          // the one their PC is currently driving.
          final list = [...raw]..sort((a, b) {
              if (a.hostAttached != b.hostAttached) {
                return a.hostAttached ? -1 : 1;
              }
              return b.lastIo.compareTo(a.lastIo);
            });
          if (list.isEmpty) {
            return Center(
              child: Padding(
                padding: const EdgeInsets.all(32),
                child: Column(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    Text(
                      t.sessionsEmptyTitle,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 8),
                    Text(
                      t.sessionsEmptyDescription,
                      textAlign: TextAlign.center,
                    ),
                  ],
                ),
              ),
            );
          }
          return ListView.separated(
            itemCount: list.length,
            separatorBuilder: (_, __) => const Divider(height: 1),
            itemBuilder: (_, i) {
              final s = list[i];
              return ListTile(
                leading: s.hostAttached
                    ? Tooltip(
                        message: t.sessionListHostAttachedTooltip,
                        child: Icon(
                          Icons.computer,
                          color: Theme.of(context).colorScheme.primary,
                        ),
                      )
                    : null,
                title: Text(s.title),
                subtitle: Text(
                  s.hostAttached
                      ? '${t.sessionListMeta(s.cols, s.rows, s.subscribers)} · ${t.sessionListHostAttached}'
                      : t.sessionListMeta(s.cols, s.rows, s.subscribers),
                ),
                trailing: IconButton(
                  icon: const Icon(Icons.close),
                  onPressed: () => _kill(s),
                ),
                onTap: () async {
                  await Navigator.of(context).push(
                    MaterialPageRoute<void>(
                      builder: (_) => TerminalWorkspace(
                        profile: widget.profile,
                        initialSession: s,
                      ),
                    ),
                  );
                  _refresh();
                },
              );
            },
          );
        },
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _create,
        icon: const Icon(Icons.add),
        label: Text(t.sessionsCreateTitle),
      ),
    );
  }
}
