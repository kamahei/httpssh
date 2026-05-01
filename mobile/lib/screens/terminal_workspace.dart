import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:xterm/xterm.dart';

import '../api/api_client.dart';
import '../l10n/generated/app_localizations.dart';
import '../models/profile.dart';
import '../models/session_info.dart';
import '../state/settings.dart';
import '../terminal/resize_policy.dart';
import '../terminal/terminal_input.dart';
import '../terminal/terminal_session.dart';
import '../terminal/themes.dart';
import '../terminal/viewport_estimate.dart';
import 'file_browser_screen.dart';

/// Multi-tab terminal workspace. Each tab owns its own [TerminalSession]
/// (independent WebSocket + xterm) and the tabs are kept alive with an
/// [IndexedStack] so that backgrounded tabs continue to receive output
/// without being rebuilt.
class TerminalWorkspace extends ConsumerStatefulWidget {
  const TerminalWorkspace({
    super.key,
    required this.profile,
    required this.initialSession,
  });

  final Profile profile;
  final SessionInfo initialSession;

  @override
  ConsumerState<TerminalWorkspace> createState() => _TerminalWorkspaceState();
}

class _TabModel {
  _TabModel({required this.session, required this.terminalSession}) {
    title = session.title;
  }

  SessionInfo session;
  TerminalSession terminalSession;
  late String title;
}

class _TerminalWorkspaceState extends ConsumerState<TerminalWorkspace>
    with WidgetsBindingObserver {
  late final ApiClient _api = ApiClient(widget.profile);
  final List<_TabModel> _tabs = [];
  int _active = 0;
  bool _fullscreen = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _tabs.add(_makeTab(widget.initialSession));
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    for (final t in _tabs) {
      t.terminalSession.removeListener(_onTabChanged);
      t.terminalSession.dispose();
    }
    super.dispose();
  }

  @override
  void didChangeMetrics() {
    // Orientation change, IME open/close, foldable hinge crossing, etc.
    // Force a rebuild so the TerminalView's RenderObject relayouts and
    // recomputes cell counts. Without this we have observed cases where
    // a rotation does not propagate through the IndexedStack to all
    // tabs, leaving the relay's ConPTY at the old dimensions.
    if (mounted) setState(() {});
  }

  bool _currentLineWrap() => ref.read(lineWrapProvider).maybeWhen(
        data: (v) => v,
        orElse: () => true,
      );

  _TabModel _makeTab(SessionInfo info) {
    final ts = TerminalSession(
      api: _api,
      session: info,
      lineWrap: _currentLineWrap(),
    );
    ts.addListener(_onTabChanged);
    return _TabModel(session: info, terminalSession: ts);
  }

  void _onTabChanged() {
    if (!mounted) return;
    setState(() {});
  }

  Future<void> _addTabFlow() async {
    final t = AppLocalizations.of(context)!;
    final action = await showModalBottomSheet<String>(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.add_circle_outline),
              title: Text(t.terminalNewSessionTitle),
              onTap: () => Navigator.pop(ctx, 'new'),
            ),
            ListTile(
              leading: const Icon(Icons.link),
              title: Text(t.terminalAttachTitle),
              onTap: () => Navigator.pop(ctx, 'attach'),
            ),
          ],
        ),
      ),
    );
    if (action == 'new') {
      await _spawnNewTab();
    } else if (action == 'attach') {
      await _attachExistingTab();
    }
  }

  Future<void> _spawnNewTab() async {
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
      final dims = estimateViewportCells(context, fontSize: fontSize);
      final cols = lineWrap
          ? remoteColsFor(
              shell: shell,
              lineWrap: true,
              visibleCols: dims.cols,
            )
          : kHorizontalScrollCols;
      final info = await _api.createSession(
        shell: shell,
        cols: cols,
        rows: dims.rows,
      );
      if (!mounted) return;
      setState(() {
        _tabs.add(_makeTab(info));
        _active = _tabs.length - 1;
      });
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('$e')));
    }
  }

  Future<void> _attachExistingTab() async {
    final t = AppLocalizations.of(context)!;
    List<SessionInfo> all;
    try {
      all = await _api.listSessions();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('$e')));
      return;
    }
    if (!mounted) return;
    final attachedIds = _tabs.map((x) => x.session.id).toSet();
    final candidates = all.where((s) => !attachedIds.contains(s.id)).toList();
    if (candidates.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(t.terminalNoOtherSessions)),
      );
      return;
    }
    final picked = await showModalBottomSheet<SessionInfo>(
      context: context,
      builder: (ctx) => SafeArea(
        child: ListView.builder(
          shrinkWrap: true,
          itemCount: candidates.length,
          itemBuilder: (_, i) {
            final s = candidates[i];
            return ListTile(
              title: Text(s.title),
              subtitle: Text(t.sessionListMeta(s.cols, s.rows, s.subscribers)),
              onTap: () => Navigator.pop(ctx, s),
            );
          },
        ),
      ),
    );
    if (picked == null || !mounted) return;
    setState(() {
      final tab = _makeTab(picked);
      tab.title = picked.title + t.terminalAttachedTabSuffix;
      _tabs.add(tab);
      _active = _tabs.length - 1;
    });
  }

  Future<void> _closeTabAt(int idx) async {
    final t = AppLocalizations.of(context)!;
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        content: Text(t.terminalCloseTabConfirm),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(t.cancel),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t.terminalCloseTab),
          ),
        ],
      ),
    );
    if (!(ok ?? false) || !mounted) return;
    setState(() {
      final tab = _tabs.removeAt(idx);
      tab.terminalSession.removeListener(_onTabChanged);
      tab.terminalSession.dispose();
      if (_tabs.isEmpty) {
        Navigator.of(context).pop();
        return;
      }
      if (_active >= _tabs.length) _active = _tabs.length - 1;
      if (idx < _active) _active--;
    });
  }

  Future<void> _openFilesAtCwd() async {
    if (_tabs.isEmpty) return;
    final tab = _tabs[_active];
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => FileBrowserScreen.forSession(
          profile: widget.profile,
          sessionId: tab.session.id,
          sessionTitle: tab.title,
        ),
      ),
    );
  }

  Future<void> _renameCurrent() async {
    final t = AppLocalizations.of(context)!;
    if (_tabs.isEmpty) return;
    final tab = _tabs[_active];
    final ctrl = TextEditingController(text: tab.title);
    final result = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t.terminalRenameTitle),
        content: TextField(
          controller: ctrl,
          autofocus: true,
          textInputAction: TextInputAction.done,
          onSubmitted: (v) => Navigator.pop(ctx, v),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(t.cancel),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, ctrl.text),
            child: Text(t.terminalRename),
          ),
        ],
      ),
    );
    if (result == null || result.trim().isEmpty || !mounted) return;
    final trimmed = result.trim();
    try {
      final updated = await _api.renameSession(tab.session.id, trimmed);
      if (!mounted) return;
      setState(() {
        tab.session = updated;
        tab.title = trimmed;
      });
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('$e')));
    }
  }

  Future<void> _openImeInput(Terminal terminal) async {
    final result = await showModalBottomSheet<_ImeInputResult>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (_) => const _ImeInputSheet(),
    );
    if (result == null || result.text.isEmpty || !mounted) return;
    terminal.textInput(
      terminalInputFromEditorText(
        result.text,
        appendEnter: result.appendEnter,
      ),
    );
  }

  void _reorderTab(int oldIdx, int newIdx) {
    if (oldIdx < newIdx) newIdx -= 1;
    setState(() {
      final item = _tabs.removeAt(oldIdx);
      _tabs.insert(newIdx, item);
      // Adjust the active index so the visible tab does not change after a
      // reorder unless the user dragged the active one.
      if (_active == oldIdx) {
        _active = newIdx;
      } else if (oldIdx < _active && _active <= newIdx) {
        _active--;
      } else if (newIdx <= _active && _active < oldIdx) {
        _active++;
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context)!;
    if (_tabs.isEmpty) {
      // Defensive: should never display, dispose path navigates back.
      return const Scaffold(body: Center(child: CircularProgressIndicator()));
    }
    final active = _tabs[_active];

    final paletteId = ref.watch(terminalPaletteProvider).maybeWhen(
          data: (id) => id,
          orElse: () => TerminalPaletteId.defaultDark,
        );
    final palette = paletteById(paletteId);
    final fontSize = ref.watch(terminalFontSizeProvider).maybeWhen(
          data: (s) => s,
          orElse: () => TerminalFontSizeNotifier.defaultSize,
        );
    final lineWrap = ref.watch(lineWrapProvider).maybeWhen(
          data: (v) => v,
          orElse: () => true,
        );
    for (final tab in _tabs) {
      tab.terminalSession.updateLineWrapMode(lineWrap);
    }
    final stateLabel = switch (active.terminalSession.state) {
      TerminalConnectionState.connecting => t.terminalReconnecting,
      TerminalConnectionState.live => '',
      TerminalConnectionState.reconnecting => t.terminalReconnecting,
      TerminalConnectionState.closed => t.terminalClosed,
    };

    return Scaffold(
      // Explicit: when the IME comes up, shrink the body so the terminal
      // and soft-key bar stay above the keyboard.
      resizeToAvoidBottomInset: true,
      appBar: _fullscreen
          ? null
          : AppBar(
              title: Text(active.title),
              actions: [
                IconButton(
                  icon: const Icon(Icons.folder_open_outlined),
                  tooltip: t.terminalOpenFilesAtCwd,
                  onPressed: _openFilesAtCwd,
                ),
                IconButton(
                  icon: const Icon(Icons.drive_file_rename_outline),
                  tooltip: t.terminalRename,
                  onPressed: _renameCurrent,
                ),
                IconButton(
                  icon: const Icon(Icons.fullscreen),
                  tooltip: t.terminalFullscreen,
                  onPressed: () => setState(() => _fullscreen = true),
                ),
              ],
              bottom: stateLabel.isEmpty
                  ? null
                  : PreferredSize(
                      preferredSize: const Size.fromHeight(28),
                      child: Container(
                        width: double.infinity,
                        color: active.terminalSession.state ==
                                TerminalConnectionState.closed
                            ? Colors.red.shade700
                            : Colors.amber.shade700,
                        padding: const EdgeInsets.symmetric(
                          horizontal: 12,
                          vertical: 4,
                        ),
                        child: Text(
                          stateLabel,
                          style: const TextStyle(color: Colors.black87),
                        ),
                      ),
                    ),
            ),
      body: Column(
        children: [
          if (!_fullscreen)
            _TabStrip(
              tabs: _tabs,
              active: _active,
              onSelect: (i) => setState(() => _active = i),
              onClose: _closeTabAt,
              onAdd: _addTabFlow,
              onReorder: _reorderTab,
            ),
          Expanded(
            child: Container(
              color: palette.theme.background,
              child: IndexedStack(
                index: _active,
                children: [
                  for (final tab in _tabs)
                    _TerminalBody(
                      key: ValueKey('body-${tab.session.id}'),
                      terminal: tab.terminalSession.terminal,
                      theme: palette.theme,
                      fontSize: fontSize,
                      lineWrap: lineWrap,
                    ),
                ],
              ),
            ),
          ),
          if (_fullscreen)
            Material(
              color: Theme.of(context).colorScheme.surfaceContainer,
              child: SafeArea(
                top: false,
                child: SizedBox(
                  height: 36,
                  child: Row(
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: [
                      IconButton(
                        icon: const Icon(Icons.fullscreen_exit),
                        tooltip: t.terminalExitFullscreen,
                        onPressed: () => setState(() => _fullscreen = false),
                      ),
                    ],
                  ),
                ),
              ),
            )
          else
            _SoftKeyBar(
              onSpecial: (s) => active.terminalSession.terminal.textInput(s),
              onImeInput: () => _openImeInput(active.terminalSession.terminal),
            ),
        ],
      ),
    );
  }
}

class _TabStrip extends StatelessWidget {
  const _TabStrip({
    required this.tabs,
    required this.active,
    required this.onSelect,
    required this.onClose,
    required this.onAdd,
    required this.onReorder,
  });

  final List<_TabModel> tabs;
  final int active;
  final ValueChanged<int> onSelect;
  final ValueChanged<int> onClose;
  final VoidCallback onAdd;
  final void Function(int oldIdx, int newIdx) onReorder;

  @override
  Widget build(BuildContext context) {
    final l = AppLocalizations.of(context)!;
    return Container(
      height: 44,
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainer,
        border: Border(
          bottom: BorderSide(color: Theme.of(context).dividerColor),
        ),
      ),
      child: Row(
        children: [
          Expanded(
            child: ReorderableListView.builder(
              scrollDirection: Axis.horizontal,
              buildDefaultDragHandles: false,
              itemCount: tabs.length,
              onReorder: onReorder,
              itemBuilder: (_, i) {
                final tab = tabs[i];
                return ReorderableDragStartListener(
                  index: i,
                  key: ValueKey('tab-${tab.session.id}'),
                  child: _TabPill(
                    tab: tab,
                    active: i == active,
                    onTap: () => onSelect(i),
                    onClose: () => onClose(i),
                  ),
                );
              },
            ),
          ),
          IconButton(
            icon: const Icon(Icons.add),
            tooltip: l.terminalAddTab,
            onPressed: onAdd,
          ),
        ],
      ),
    );
  }
}

class _TabPill extends StatelessWidget {
  const _TabPill({
    required this.tab,
    required this.active,
    required this.onTap,
    required this.onClose,
  });

  final _TabModel tab;
  final bool active;
  final VoidCallback onTap;
  final VoidCallback onClose;

  Color get _dotColor => switch (tab.terminalSession.state) {
        TerminalConnectionState.live => Colors.green,
        TerminalConnectionState.connecting ||
        TerminalConnectionState.reconnecting =>
          Colors.amber,
        TerminalConnectionState.closed => Colors.red,
      };

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final bg =
        active ? theme.colorScheme.primaryContainer : theme.colorScheme.surface;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 4),
      child: Material(
        color: bg,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(20),
          side: BorderSide(
            color: active ? theme.colorScheme.primary : theme.dividerColor,
          ),
        ),
        child: InkWell(
          borderRadius: BorderRadius.circular(20),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.only(left: 12, right: 4),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Container(
                  width: 8,
                  height: 8,
                  decoration: BoxDecoration(
                    color: _dotColor,
                    shape: BoxShape.circle,
                  ),
                ),
                const SizedBox(width: 8),
                ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 160),
                  child: Text(
                    tab.title,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      color: active
                          ? theme.colorScheme.onPrimaryContainer
                          : theme.colorScheme.onSurface,
                    ),
                  ),
                ),
                IconButton(
                  iconSize: 16,
                  visualDensity: VisualDensity.compact,
                  splashRadius: 14,
                  onPressed: onClose,
                  icon: const Icon(Icons.close),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _TerminalBody extends StatelessWidget {
  const _TerminalBody({
    super.key,
    required this.terminal,
    required this.theme,
    required this.fontSize,
    required this.lineWrap,
  });

  final Terminal terminal;
  final TerminalTheme theme;
  final double fontSize;
  final bool lineWrap;

  @override
  Widget build(BuildContext context) {
    if (lineWrap) {
      // Wrap mode: trust xterm to measure the cell size from the font
      // and resize the Terminal to fit the viewport exactly. Line wrap
      // happens at the visible right edge.
      return TerminalView(
        terminal,
        theme: theme,
        textStyle: TerminalStyle(fontSize: fontSize),
        autofocus: true,
        onSecondaryTapDown: (_, __) async {
          final data = await Clipboard.getData('text/plain');
          final text = data?.text;
          if (text != null && text.isNotEmpty) terminal.textInput(text);
        },
      );
    }

    // Scroll mode: pin the terminal width at kHorizontalScrollCols.
    // We disable xterm's autoResize and call terminal.resize(...)
    // ourselves so that the column count is independent of how wide
    // the host happens to render each cell — without this, xterm's
    // own measurement chooses a column count from the SizedBox width
    // that may differ from kHorizontalScrollCols and the
    // shell would format output for the "wrong" width.
    return LayoutBuilder(
      builder: (context, c) {
        final cellW = fontSize * 0.7; // generous overestimate to avoid clipping
        final cellH = fontSize * 1.2;
        final width = kHorizontalScrollCols * cellW;
        final rows = (c.maxHeight / cellH).floor().clamp(5, 200);
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (terminal.viewWidth != kHorizontalScrollCols ||
              terminal.viewHeight != rows) {
            terminal.resize(kHorizontalScrollCols, rows);
          }
        });
        return SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          child: SizedBox(
            width: width,
            height: c.maxHeight,
            child: TerminalView(
              terminal,
              theme: theme,
              textStyle: TerminalStyle(fontSize: fontSize),
              autoResize: false, // we control the dimensions explicitly
              autofocus: true,
              onSecondaryTapDown: (_, __) async {
                final data = await Clipboard.getData('text/plain');
                final text = data?.text;
                if (text != null && text.isNotEmpty) terminal.textInput(text);
              },
            ),
          ),
        );
      },
    );
  }
}

class _SoftKeyBar extends StatefulWidget {
  const _SoftKeyBar({required this.onSpecial, required this.onImeInput});
  final void Function(String) onSpecial;
  final VoidCallback onImeInput;

  @override
  State<_SoftKeyBar> createState() => _SoftKeyBarState();
}

class _SoftKeyBarState extends State<_SoftKeyBar> {
  bool _ctrlSticky = false;

  void _send(String s) {
    if (_ctrlSticky && s.isNotEmpty) {
      final c = s.codeUnitAt(0);
      if (c >= 0x40 && c <= 0x7f) {
        s = String.fromCharCode(c & 0x1f);
      }
      setState(() => _ctrlSticky = false);
    }
    widget.onSpecial(s);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context)!;
    return Material(
      color: Theme.of(context).colorScheme.surfaceContainer,
      child: SafeArea(
        top: false,
        child: SizedBox(
          height: 44,
          child: ListView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.symmetric(horizontal: 8),
            children: [
              _IconKeyButton(
                icon: Icons.keyboard_alt_outlined,
                tooltip: t.terminalImeInput,
                onTap: widget.onImeInput,
              ),
              _KeyButton(label: 'Tab', onTap: () => _send('\t')),
              _KeyButton(label: 'Esc', onTap: () => _send('\x1b')),
              _KeyButton(
                label: 'Ctrl',
                selected: _ctrlSticky,
                onTap: () => setState(() => _ctrlSticky = !_ctrlSticky),
              ),
              _KeyButton(label: '↑', onTap: () => _send('\x1b[A')),
              _KeyButton(label: '↓', onTap: () => _send('\x1b[B')),
              _KeyButton(label: '←', onTap: () => _send('\x1b[D')),
              _KeyButton(label: '→', onTap: () => _send('\x1b[C')),
              _KeyButton(label: '^C', onTap: () => _send('\x03')),
              _KeyButton(label: '^L', onTap: () => _send('\x0c')),
              _KeyButton(label: '^Z', onTap: () => _send('\x1a')),
            ],
          ),
        ),
      ),
    );
  }
}

class _ImeInputResult {
  const _ImeInputResult({required this.text, required this.appendEnter});

  final String text;
  final bool appendEnter;
}

class _ImeInputSheet extends StatefulWidget {
  const _ImeInputSheet();

  @override
  State<_ImeInputSheet> createState() => _ImeInputSheetState();
}

class _ImeInputSheetState extends State<_ImeInputSheet> {
  final _controller = TextEditingController();
  final _focusNode = FocusNode();
  bool _appendEnter = true;

  @override
  void initState() {
    super.initState();
    _controller.addListener(_onTextChanged);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _focusNode.requestFocus();
    });
  }

  @override
  void dispose() {
    _controller.removeListener(_onTextChanged);
    _controller.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  void _onTextChanged() {
    setState(() {});
  }

  void _send() {
    final text = _controller.text;
    if (text.isEmpty) return;
    Navigator.pop(
      context,
      _ImeInputResult(text: text, appendEnter: _appendEnter),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context)!;
    final theme = Theme.of(context);
    return AnimatedPadding(
      duration: const Duration(milliseconds: 160),
      curve: Curves.easeOut,
      padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
      child: Material(
        color: theme.colorScheme.surface,
        child: SafeArea(
          top: false,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        t.terminalImeInputTitle,
                        style: theme.textTheme.titleMedium,
                      ),
                    ),
                    IconButton(
                      icon: const Icon(Icons.close),
                      tooltip: t.cancel,
                      onPressed: () => Navigator.pop(context),
                    ),
                  ],
                ),
                const SizedBox(height: 8),
                ConstrainedBox(
                  constraints: const BoxConstraints(maxHeight: 220),
                  child: TextField(
                    controller: _controller,
                    focusNode: _focusNode,
                    minLines: 4,
                    maxLines: null,
                    keyboardType: TextInputType.multiline,
                    textInputAction: TextInputAction.newline,
                    decoration: InputDecoration(
                      border: const OutlineInputBorder(),
                      hintText: t.terminalImeInputHint,
                    ),
                  ),
                ),
                const SizedBox(height: 8),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: Text(t.terminalImeAppendEnter),
                  value: _appendEnter,
                  onChanged: (value) => setState(() => _appendEnter = value),
                ),
                const SizedBox(height: 8),
                FilledButton.icon(
                  icon: const Icon(Icons.send),
                  label: Text(t.terminalImeSend),
                  onPressed: _controller.text.isEmpty ? null : _send,
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _IconKeyButton extends StatelessWidget {
  const _IconKeyButton({
    required this.icon,
    required this.tooltip,
    required this.onTap,
  });

  final IconData icon;
  final String tooltip;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 6),
      child: IconButton.filledTonal(
        iconSize: 20,
        padding: EdgeInsets.zero,
        style: IconButton.styleFrom(
          backgroundColor: theme.colorScheme.surfaceContainerHighest,
          fixedSize: const Size(40, 32),
          minimumSize: const Size(40, 32),
        ),
        icon: Icon(icon),
        tooltip: tooltip,
        onPressed: onTap,
      ),
    );
  }
}

class _KeyButton extends StatelessWidget {
  const _KeyButton({
    required this.label,
    required this.onTap,
    this.selected = false,
  });
  final String label;
  final VoidCallback onTap;
  final bool selected;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 6),
      child: TextButton(
        style: TextButton.styleFrom(
          backgroundColor: selected
              ? theme.colorScheme.primaryContainer
              : theme.colorScheme.surfaceContainerHighest,
          padding: const EdgeInsets.symmetric(horizontal: 12),
        ),
        onPressed: onTap,
        child: Text(label, style: const TextStyle(fontFamily: 'monospace')),
      ),
    );
  }
}
