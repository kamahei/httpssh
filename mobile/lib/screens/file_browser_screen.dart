import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_highlighting/themes/github-dark.dart';
import 'package:flutter_highlighting/themes/github.dart';

import '../api/api_client.dart';
import '../files/file_bookmarks.dart';
import '../files/syntax_highlighting.dart';
import '../files/text_search.dart';
import '../l10n/generated/app_localizations.dart';
import '../models/file_browser.dart';
import '../models/profile.dart';

class FileBrowserScreen extends StatefulWidget {
  const FileBrowserScreen({super.key, required this.profile})
      : sessionId = null,
        sessionTitle = null;

  /// Opens the browser in session-scoped mode: paths are resolved
  /// relative to the session's last-known working directory rather than
  /// any configured file root. Bookmarks, root selection, and "add
  /// bookmark" are hidden in this mode because the CWD is transient.
  const FileBrowserScreen.forSession({
    super.key,
    required this.profile,
    required String this.sessionId,
    this.sessionTitle,
  });

  final Profile profile;
  final String? sessionId;
  final String? sessionTitle;

  bool get isSessionScoped => sessionId != null;

  @override
  State<FileBrowserScreen> createState() => _FileBrowserScreenState();
}

class _FileBrowserScreenState extends State<FileBrowserScreen> {
  late final ApiClient _api = ApiClient(widget.profile);
  final _bookmarkStore = const FileBookmarkStore();

  List<FileRootInfo> _roots = const [];
  List<FileEntry> _entries = const [];
  List<FileBookmark> _bookmarks = const [];
  String? _rootId;
  String _path = '';
  String _sessionCwd = '';
  bool _loading = true;
  String? _error;

  FileRootInfo? get _currentRoot {
    for (final root in _roots) {
      if (root.id == _rootId) return root;
    }
    return null;
  }

  @override
  void initState() {
    super.initState();
    _loadInitial();
  }

  Future<void> _loadInitial() async {
    if (widget.isSessionScoped) {
      await _loadSessionCwd();
      await _openPath('');
      return;
    }
    final bookmarks = await _bookmarkStore.load(widget.profile.id);
    if (!mounted) return;
    setState(() => _bookmarks = bookmarks);
    await _loadRoots();
  }

  Future<void> _loadSessionCwd() async {
    try {
      final info = await _api.getSession(widget.sessionId!);
      if (!mounted) return;
      setState(() => _sessionCwd = info.cwd);
    } catch (_) {
      // Non-fatal: the listing itself surfaces a clearer error.
    }
  }

  Future<void> _loadRoots() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final roots = await _api.listFileRoots();
      if (!mounted) return;
      setState(() {
        _roots = roots;
        _rootId = roots.isEmpty ? null : (_rootId ?? roots.first.id);
      });
      if (roots.isNotEmpty) {
        await _openPath(_path);
      } else if (mounted) {
        setState(() => _loading = false);
      }
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = '$e';
        _loading = false;
      });
    }
  }

  Future<void> _openPath(String path, {String? rootId}) async {
    if (widget.isSessionScoped) {
      setState(() {
        _loading = true;
        _error = null;
      });
      try {
        final result = await _api.listSessionFiles(
          sessionId: widget.sessionId!,
          path: path,
        );
        if (!mounted) return;
        setState(() {
          _path = result.path;
          _entries = result.entries;
          _loading = false;
        });
      } catch (e) {
        if (!mounted) return;
        setState(() {
          _error = '$e';
          _loading = false;
        });
      }
      return;
    }
    final targetRoot = rootId ?? _rootId;
    if (targetRoot == null) return;
    setState(() {
      _loading = true;
      _error = null;
      _rootId = targetRoot;
    });
    try {
      final result = await _api.listFiles(root: targetRoot, path: path);
      if (!mounted) return;
      setState(() {
        _path = result.path;
        _entries = result.entries;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = '$e';
        _loading = false;
      });
    }
  }

  Future<void> _openEntry(FileEntry entry) async {
    if (entry.isDirectory) {
      await _openPath(entry.path);
      return;
    }
    if (!mounted) return;
    if (widget.isSessionScoped) {
      await Navigator.of(context).push(
        MaterialPageRoute<void>(
          builder: (_) => TextViewerScreen.forSession(
            profile: widget.profile,
            sessionId: widget.sessionId!,
            path: entry.path,
            title: entry.name,
          ),
        ),
      );
      return;
    }
    final root = _rootId;
    if (root == null) return;
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => TextViewerScreen(
          profile: widget.profile,
          root: root,
          path: entry.path,
          title: entry.name,
        ),
      ),
    );
  }

  Future<void> _showPathDialog() async {
    final t = AppLocalizations.of(context)!;
    final controller = TextEditingController(text: _path);
    final path = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t.filesOpenPath),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: InputDecoration(
            labelText: t.filesPath,
            hintText: t.filesPathHint,
          ),
          onSubmitted: (value) => Navigator.pop(ctx, value),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(t.cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, controller.text),
            child: Text(t.ok),
          ),
        ],
      ),
    );
    controller.dispose();
    if (path == null || !mounted) return;
    await _openPath(path.trim());
  }

  Future<void> _addBookmark() async {
    final t = AppLocalizations.of(context)!;
    final root = _currentRoot;
    if (root == null) return;
    final label = _path.isEmpty ? root.name : '${root.name}/$_path';
    final bookmark = FileBookmark(rootId: root.id, path: _path, label: label);
    final bookmarks = await _bookmarkStore.add(widget.profile.id, bookmark);
    if (!mounted) return;
    setState(() => _bookmarks = bookmarks);
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(t.filesBookmarkAdded)),
    );
  }

  Future<void> _removeBookmark(FileBookmark bookmark) async {
    final bookmarks = await _bookmarkStore.remove(widget.profile.id, bookmark);
    if (!mounted) return;
    setState(() => _bookmarks = bookmarks);
  }

  Future<void> _showBookmarks() async {
    final t = AppLocalizations.of(context)!;
    await showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (ctx) => SafeArea(
        child: _bookmarks.isEmpty
            ? Padding(
                padding: const EdgeInsets.all(24),
                child: Text(t.filesNoBookmarks),
              )
            : ListView.separated(
                shrinkWrap: true,
                itemCount: _bookmarks.length,
                separatorBuilder: (_, __) => const Divider(height: 1),
                itemBuilder: (_, i) {
                  final bookmark = _bookmarks[i];
                  return ListTile(
                    leading: const Icon(Icons.bookmark_outline),
                    title: Text(bookmark.label),
                    subtitle: Text(bookmark.path.isEmpty ? '/' : bookmark.path),
                    trailing: IconButton(
                      icon: const Icon(Icons.delete_outline),
                      tooltip: t.filesRemoveBookmark,
                      onPressed: () async {
                        await _removeBookmark(bookmark);
                        if (ctx.mounted) Navigator.pop(ctx);
                      },
                    ),
                    onTap: () {
                      Navigator.pop(ctx);
                      _openPath(bookmark.path, rootId: bookmark.rootId);
                    },
                  );
                },
              ),
      ),
    );
  }

  Future<void> _goParent() async {
    if (_path.isEmpty) return;
    final parts = _path.split('/')..removeWhere((p) => p.isEmpty);
    if (parts.isNotEmpty) parts.removeLast();
    await _openPath(parts.join('/'));
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context)!;
    final title = widget.isSessionScoped
        ? t.filesSessionTitle(widget.sessionTitle ?? '')
        : t.filesTitle;
    return Scaffold(
      appBar: AppBar(
        title: Text(title),
        actions: [
          if (!widget.isSessionScoped)
            IconButton(
              icon: const Icon(Icons.bookmarks_outlined),
              tooltip: t.filesBookmarks,
              onPressed: _showBookmarks,
            ),
          IconButton(
            icon: const Icon(Icons.refresh),
            tooltip: t.filesRefresh,
            onPressed: () => _openPath(_path),
          ),
        ],
      ),
      body: _buildBody(context, t),
    );
  }

  Widget _buildBody(BuildContext context, AppLocalizations t) {
    if (_loading && _entries.isEmpty && _error == null) {
      return const Center(child: CircularProgressIndicator());
    }
    if (!widget.isSessionScoped) {
      if (_error != null && _roots.isEmpty) {
        return _ErrorState(message: _error!, onRetry: _loadRoots);
      }
      if (_roots.isEmpty) {
        return _EmptyState(
          title: t.filesNoRootsTitle,
          description: t.filesNoRootsDescription,
        );
      }
    }
    final rootLabel = widget.isSessionScoped
        ? (_sessionCwd.isEmpty ? t.filesSessionCwdUnknown : _sessionCwd)
        : (_currentRoot?.name ?? '');
    return Column(
      children: [
        Material(
          color: Theme.of(context).colorScheme.surfaceContainer,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(12, 8, 8, 8),
            child: Row(
              children: [
                Expanded(
                  child: widget.isSessionScoped
                      ? Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 4),
                          child: Text(
                            rootLabel,
                            overflow: TextOverflow.ellipsis,
                            style: Theme.of(context).textTheme.titleSmall,
                          ),
                        )
                      : DropdownButtonHideUnderline(
                          child: DropdownButton<String>(
                            isExpanded: true,
                            value: _rootId,
                            items: [
                              for (final root in _roots)
                                DropdownMenuItem(
                                  value: root.id,
                                  child: Text(root.name),
                                ),
                            ],
                            onChanged: (value) {
                              if (value != null) _openPath('', rootId: value);
                            },
                          ),
                        ),
                ),
                IconButton(
                  icon: const Icon(Icons.drive_folder_upload_outlined),
                  tooltip: t.filesParent,
                  onPressed: _path.isEmpty ? null : _goParent,
                ),
                IconButton(
                  icon: const Icon(Icons.edit_location_alt_outlined),
                  tooltip: t.filesOpenPath,
                  onPressed: _showPathDialog,
                ),
                if (!widget.isSessionScoped)
                  IconButton(
                    icon: const Icon(Icons.bookmark_add_outlined),
                    tooltip: t.filesAddBookmark,
                    onPressed: _addBookmark,
                  ),
              ],
            ),
          ),
        ),
        _Breadcrumbs(
          rootName: rootLabel,
          path: _path,
          onOpen: _openPath,
        ),
        if (_error != null)
          Padding(
            padding: const EdgeInsets.all(12),
            child: Text(
              _error!,
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
          ),
        Expanded(
          child: Stack(
            children: [
              if (_entries.isEmpty && !_loading && _error == null)
                _EmptyState(
                  title: t.filesNoEntries,
                  description: t.filesCurrentPath(_path.isEmpty ? '/' : _path),
                )
              else
                ListView.separated(
                  itemCount: _entries.length,
                  separatorBuilder: (_, __) => const Divider(height: 1),
                  itemBuilder: (_, i) {
                    final entry = _entries[i];
                    return ListTile(
                      leading: Icon(
                        entry.isDirectory
                            ? Icons.folder_outlined
                            : Icons.description_outlined,
                      ),
                      title: Text(entry.name),
                      subtitle: Text(_entrySubtitle(context, t, entry)),
                      onTap: () => _openEntry(entry),
                    );
                  },
                ),
              if (_loading)
                const Align(
                  alignment: Alignment.topCenter,
                  child: LinearProgressIndicator(),
                ),
            ],
          ),
        ),
      ],
    );
  }

  String _entrySubtitle(
    BuildContext context,
    AppLocalizations t,
    FileEntry entry,
  ) {
    final date = MaterialLocalizations.of(context)
        .formatShortDate(entry.modifiedAt.toLocal());
    if (entry.isDirectory) return t.filesDirectoryMeta(date);
    return t.filesFileMeta(_formatSize(t, entry.size), date);
  }

  String _formatSize(AppLocalizations t, int size) {
    if (size < 1024) return t.filesSizeBytes(size);
    if (size < 1024 * 1024) {
      return t.filesSizeKib((size / 1024).toStringAsFixed(1));
    }
    return t.filesSizeMib((size / (1024 * 1024)).toStringAsFixed(1));
  }
}

class _Breadcrumbs extends StatelessWidget {
  const _Breadcrumbs({
    required this.rootName,
    required this.path,
    required this.onOpen,
  });

  final String rootName;
  final String path;
  final ValueChanged<String> onOpen;

  @override
  Widget build(BuildContext context) {
    final segments = path.split('/')..removeWhere((s) => s.isEmpty);
    return SizedBox(
      height: 44,
      child: ListView(
        scrollDirection: Axis.horizontal,
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
        children: [
          ActionChip(
            label: Text(rootName.isEmpty ? '/' : rootName),
            onPressed: () => onOpen(''),
          ),
          for (var i = 0; i < segments.length; i++) ...[
            const Padding(
              padding: EdgeInsets.symmetric(horizontal: 4),
              child: Center(child: Text('/')),
            ),
            ActionChip(
              label: Text(segments[i]),
              onPressed: () => onOpen(segments.take(i + 1).join('/')),
            ),
          ],
        ],
      ),
    );
  }
}

class TextViewerScreen extends StatefulWidget {
  const TextViewerScreen({
    super.key,
    required this.profile,
    required String this.root,
    required this.path,
    required this.title,
  }) : sessionId = null;

  /// Reads a text file under a session's last-known working directory.
  const TextViewerScreen.forSession({
    super.key,
    required this.profile,
    required String this.sessionId,
    required this.path,
    required this.title,
  }) : root = null;

  final Profile profile;
  final String? root;
  final String? sessionId;
  final String path;
  final String title;

  bool get isSessionScoped => sessionId != null;

  @override
  State<TextViewerScreen> createState() => _TextViewerScreenState();
}

class _TextViewerScreenState extends State<TextViewerScreen> {
  late final ApiClient _api = ApiClient(widget.profile);
  late final Future<FileDocument> _future;
  final _searchController = TextEditingController();
  FileDocument? _document;
  List<TextMatch> _matches = const [];
  int _currentMatch = -1;
  bool _syntaxHighlight = true;

  @override
  void initState() {
    super.initState();
    _future = widget.isSessionScoped
        ? _api.readSessionFile(
            sessionId: widget.sessionId!,
            path: widget.path,
          )
        : _api.readFile(root: widget.root!, path: widget.path);
    _future.then(
      (doc) {
        if (!mounted) return;
        setState(() => _document = doc);
        _refreshMatches();
      },
      onError: (_) {},
    );
    _searchController.addListener(_refreshMatches);
  }

  @override
  void dispose() {
    _searchController.removeListener(_refreshMatches);
    _searchController.dispose();
    super.dispose();
  }

  void _refreshMatches() {
    final doc = _document;
    if (doc == null) return;
    final matches = findTextMatches(doc.content, _searchController.text);
    setState(() {
      _matches = matches;
      _currentMatch = matches.isEmpty ? -1 : 0;
    });
  }

  void _moveMatch(bool forward) {
    setState(() {
      _currentMatch = nextTextMatchIndex(
        _currentMatch,
        _matches.length,
        forward: forward,
      );
    });
  }

  Future<void> _copyAll() async {
    final t = AppLocalizations.of(context)!;
    final content = _document?.content;
    if (content == null) return;
    await Clipboard.setData(ClipboardData(text: content));
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(t.filesCopied)),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context)!;
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.title),
        actions: [
          IconButton(
            icon: const Icon(Icons.copy_all_outlined),
            tooltip: t.filesCopy,
            onPressed: _document == null ? null : _copyAll,
          ),
          IconButton(
            icon: Icon(
              _syntaxHighlight
                  ? Icons.palette_outlined
                  : Icons.format_color_reset_outlined,
            ),
            tooltip: _syntaxHighlight
                ? t.filesDisableSyntaxHighlight
                : t.filesEnableSyntaxHighlight,
            onPressed: () {
              setState(() => _syntaxHighlight = !_syntaxHighlight);
            },
          ),
        ],
      ),
      body: FutureBuilder<FileDocument>(
        future: _future,
        builder: (context, snapshot) {
          if (snapshot.connectionState == ConnectionState.waiting) {
            return const Center(child: CircularProgressIndicator());
          }
          if (snapshot.hasError) {
            return _ErrorState(message: '${snapshot.error}');
          }
          final doc = snapshot.data!;
          final language = _syntaxHighlight && _searchController.text.isEmpty
              ? highlightLanguageForPath(doc.path)
              : null;
          return Column(
            children: [
              Material(
                color: Theme.of(context).colorScheme.surfaceContainer,
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(12, 8, 8, 8),
                  child: Row(
                    children: [
                      Expanded(
                        child: TextField(
                          controller: _searchController,
                          decoration: InputDecoration(
                            isDense: true,
                            prefixIcon: const Icon(Icons.search),
                            labelText: t.filesSearch,
                            border: const OutlineInputBorder(),
                          ),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Text(
                        t.filesSearchMatches(
                          _matches.isEmpty ? 0 : _currentMatch + 1,
                          _matches.length,
                        ),
                      ),
                      IconButton(
                        icon: const Icon(Icons.keyboard_arrow_up),
                        tooltip: t.filesSearchPrevious,
                        onPressed:
                            _matches.isEmpty ? null : () => _moveMatch(false),
                      ),
                      IconButton(
                        icon: const Icon(Icons.keyboard_arrow_down),
                        tooltip: t.filesSearchNext,
                        onPressed:
                            _matches.isEmpty ? null : () => _moveMatch(true),
                      ),
                    ],
                  ),
                ),
              ),
              Padding(
                padding:
                    const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                child: Align(
                  alignment: Alignment.centerLeft,
                  child: Text(
                    t.filesViewerMeta(
                      doc.encoding,
                      _formatViewerSize(t, doc.size),
                    ),
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ),
              ),
              Expanded(
                child: _TextContent(
                  doc: doc,
                  matches: _matches,
                  currentMatch: _currentMatch,
                  syntaxLanguage: language,
                ),
              ),
            ],
          );
        },
      ),
    );
  }

  String _formatViewerSize(AppLocalizations t, int size) {
    if (size < 1024) return t.filesSizeBytes(size);
    if (size < 1024 * 1024) {
      return t.filesSizeKib((size / 1024).toStringAsFixed(1));
    }
    return t.filesSizeMib((size / (1024 * 1024)).toStringAsFixed(1));
  }
}

class _TextContent extends StatelessWidget {
  const _TextContent({
    required this.doc,
    required this.matches,
    required this.currentMatch,
    required this.syntaxLanguage,
  });

  final FileDocument doc;
  final List<TextMatch> matches;
  final int currentMatch;
  final String? syntaxLanguage;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final baseStyle = TextStyle(
      fontFamily: 'monospace',
      fontSize: 13,
      color: theme.colorScheme.onSurface,
    );
    final syntaxTheme =
        theme.brightness == Brightness.dark ? githubDarkTheme : githubTheme;
    return LayoutBuilder(
      builder: (context, constraints) => Scrollbar(
        child: SingleChildScrollView(
          child: SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: ConstrainedBox(
              constraints: BoxConstraints(minWidth: constraints.maxWidth),
              child: Padding(
                padding: const EdgeInsets.all(12),
                child: SelectableText.rich(
                  TextSpan(
                    style: baseStyle,
                    children: _spans(theme, baseStyle, syntaxTheme),
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  List<TextSpan> _spans(
    ThemeData theme,
    TextStyle baseStyle,
    Map<String, TextStyle> syntaxTheme,
  ) {
    final language = syntaxLanguage;
    if (matches.isEmpty && language != null) {
      try {
        return syntaxHighlightSpans(
          content: doc.content,
          language: language,
          theme: syntaxTheme,
          baseStyle: baseStyle,
        );
      } catch (_) {
        return [TextSpan(text: doc.content)];
      }
    }
    if (matches.isEmpty) return [TextSpan(text: doc.content)];
    final spans = <TextSpan>[];
    var cursor = 0;
    for (var i = 0; i < matches.length; i++) {
      final match = matches[i];
      if (cursor < match.start) {
        spans.add(TextSpan(text: doc.content.substring(cursor, match.start)));
      }
      spans.add(
        TextSpan(
          text: doc.content.substring(match.start, match.end),
          style: baseStyle.copyWith(
            backgroundColor: i == currentMatch
                ? theme.colorScheme.primaryContainer
                : theme.colorScheme.secondaryContainer,
            color: i == currentMatch
                ? theme.colorScheme.onPrimaryContainer
                : theme.colorScheme.onSecondaryContainer,
          ),
        ),
      );
      cursor = match.end;
    }
    if (cursor < doc.content.length) {
      spans.add(TextSpan(text: doc.content.substring(cursor)));
    }
    return spans;
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.message, this.onRetry});

  final String message;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context)!;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(message, textAlign: TextAlign.center),
            if (onRetry != null) ...[
              const SizedBox(height: 12),
              FilledButton(
                onPressed: onRetry,
                child: Text(t.filesRefresh),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState({required this.title, required this.description});

  final String title;
  final String description;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(title, style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 8),
            Text(description, textAlign: TextAlign.center),
          ],
        ),
      ),
    );
  }
}
