import 'dart:convert';

import 'package:shared_preferences/shared_preferences.dart';

class FileBookmark {
  const FileBookmark({
    required this.rootId,
    required this.path,
    required this.label,
  });

  factory FileBookmark.fromJson(Map<String, dynamic> json) {
    return FileBookmark(
      rootId: json['rootId'] as String? ?? '',
      path: json['path'] as String? ?? '',
      label: json['label'] as String? ?? '',
    );
  }

  final String rootId;
  final String path;
  final String label;

  Map<String, dynamic> toJson() => {
        'rootId': rootId,
        'path': path,
        'label': label,
      };

  bool sameTarget(FileBookmark other) {
    return rootId == other.rootId && path == other.path;
  }
}

class FileBookmarkStore {
  const FileBookmarkStore();

  static String _key(String profileId) => 'files.bookmarks.$profileId';

  Future<List<FileBookmark>> load(String profileId) async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_key(profileId));
    if (raw == null || raw.isEmpty) return const [];
    try {
      final list = (jsonDecode(raw) as List).cast<Map<String, dynamic>>();
      return list.map(FileBookmark.fromJson).toList();
    } catch (_) {
      return const [];
    }
  }

  Future<void> save(String profileId, List<FileBookmark> bookmarks) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(
      _key(profileId),
      jsonEncode(bookmarks.map((b) => b.toJson()).toList()),
    );
  }

  Future<List<FileBookmark>> add(
    String profileId,
    FileBookmark bookmark,
  ) async {
    final bookmarks = await load(profileId);
    final next = [
      bookmark,
      for (final b in bookmarks)
        if (!b.sameTarget(bookmark)) b,
    ];
    await save(profileId, next);
    return next;
  }

  Future<List<FileBookmark>> remove(
    String profileId,
    FileBookmark bookmark,
  ) async {
    final bookmarks = await load(profileId);
    final next = [
      for (final b in bookmarks)
        if (!b.sameTarget(bookmark)) b,
    ];
    await save(profileId, next);
    return next;
  }
}
