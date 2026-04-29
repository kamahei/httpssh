class FileRootInfo {
  const FileRootInfo({required this.id, required this.name});

  factory FileRootInfo.fromJson(Map<String, dynamic> json) {
    return FileRootInfo(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
    );
  }

  final String id;
  final String name;
}

class FileEntry {
  const FileEntry({
    required this.name,
    required this.path,
    required this.type,
    required this.size,
    required this.modifiedAt,
  });

  factory FileEntry.fromJson(Map<String, dynamic> json) {
    return FileEntry(
      name: json['name'] as String? ?? '',
      path: json['path'] as String? ?? '',
      type: json['type'] as String? ?? 'file',
      size: (json['size'] as num? ?? 0).toInt(),
      modifiedAt: DateTime.parse(
        json['modifiedAt'] as String? ??
            DateTime.fromMillisecondsSinceEpoch(0).toIso8601String(),
      ),
    );
  }

  final String name;
  final String path;
  final String type;
  final int size;
  final DateTime modifiedAt;

  bool get isDirectory => type == 'directory';
}

class FileListResult {
  const FileListResult({
    required this.root,
    required this.path,
    required this.entries,
  });

  factory FileListResult.fromJson(Map<String, dynamic> json) {
    final entries = (json['entries'] as List? ?? const [])
        .cast<Map<String, dynamic>>()
        .map(FileEntry.fromJson)
        .toList();
    return FileListResult(
      root: json['root'] as String? ?? '',
      path: json['path'] as String? ?? '',
      entries: entries,
    );
  }

  final String root;
  final String path;
  final List<FileEntry> entries;
}

class FileDocument {
  const FileDocument({
    required this.root,
    required this.path,
    required this.name,
    required this.size,
    required this.modifiedAt,
    required this.encoding,
    required this.content,
  });

  factory FileDocument.fromJson(Map<String, dynamic> json) {
    return FileDocument(
      root: json['root'] as String? ?? '',
      path: json['path'] as String? ?? '',
      name: json['name'] as String? ?? '',
      size: (json['size'] as num? ?? 0).toInt(),
      modifiedAt: DateTime.parse(
        json['modifiedAt'] as String? ??
            DateTime.fromMillisecondsSinceEpoch(0).toIso8601String(),
      ),
      encoding: json['encoding'] as String? ?? '',
      content: json['content'] as String? ?? '',
    );
  }

  final String root;
  final String path;
  final String name;
  final int size;
  final DateTime modifiedAt;
  final String encoding;
  final String content;
}
