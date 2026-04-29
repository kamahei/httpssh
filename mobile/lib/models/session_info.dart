/// Mirror of `session.SessionInfo` returned by the relay's REST API.
class SessionInfo {
  SessionInfo({
    required this.id,
    required this.title,
    required this.shell,
    required this.cols,
    required this.rows,
    required this.createdAt,
    required this.lastIo,
    required this.subscribers,
  });

  factory SessionInfo.fromJson(Map<String, dynamic> json) => SessionInfo(
        id: json['id'] as String,
        title: json['title'] as String? ?? '',
        shell: json['shell'] as String? ?? '',
        cols: (json['cols'] as num?)?.toInt() ?? 80,
        rows: (json['rows'] as num?)?.toInt() ?? 24,
        createdAt: DateTime.tryParse(json['createdAt'] as String? ?? '') ??
            DateTime.now(),
        lastIo: DateTime.tryParse(json['lastIo'] as String? ?? '') ??
            DateTime.now(),
        subscribers: (json['subscribers'] as num?)?.toInt() ?? 0,
      );

  final String id;
  final String title;
  final String shell;
  final int cols;
  final int rows;
  final DateTime createdAt;
  final DateTime lastIo;
  final int subscribers;
}
