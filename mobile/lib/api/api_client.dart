import 'package:dio/dio.dart';

import '../models/profile.dart';
import '../models/session_info.dart';

/// REST client for the relay's `/api/...` surface. One instance per
/// connected profile.
///
/// The relay always requires the LAN bearer. Depending on the profile's
/// auth mode, this client adds Cloudflare Service Token headers or a
/// session cookie ON TOP of the bearer for the Cloudflare edge layer.
class ApiClient {
  ApiClient(this.profile)
      : _dio = Dio(
          BaseOptions(
            baseUrl: profile.baseUrl,
            connectTimeout: const Duration(seconds: 10),
            receiveTimeout: const Duration(seconds: 10),
            headers: _buildHeaders(profile),
          ),
        );

  final Profile profile;
  final Dio _dio;

  static Map<String, String> _buildHeaders(Profile p) {
    final h = <String, String>{};

    // LAN bearer is always required by the relay. It is sent in every
    // mode; modes only differ in what they layer on top for Cloudflare.
    if (p.lanBearer != null && p.lanBearer!.isNotEmpty) {
      h['Authorization'] = 'Bearer ${p.lanBearer}';
    }

    switch (p.authMode) {
      case AuthMode.bearerOnly:
        // Nothing extra. Browser users reaching us through Cloudflare
        // also use this mode — the CF_Authorization cookie is set by
        // Cloudflare automatically and travels alongside the bearer.
        break;
      case AuthMode.bearerPlusServiceToken:
        if (p.cfClientId != null) h['CF-Access-Client-Id'] = p.cfClientId!;
        if (p.cfClientSecret != null) {
          h['CF-Access-Client-Secret'] = p.cfClientSecret!;
        }
      case AuthMode.bearerPlusBrowserSso:
        if (p.sessionCookie != null && p.sessionCookie!.isNotEmpty) {
          h['Cookie'] = p.sessionCookie!;
        }
    }
    return h;
  }

  Future<Map<String, dynamic>> health() async {
    final res = await _dio.get<Map<String, dynamic>>('/api/health');
    return res.data ?? const {};
  }

  Future<List<SessionInfo>> listSessions() async {
    final res = await _dio.get<Map<String, dynamic>>('/api/sessions');
    final list = (res.data?['sessions'] as List? ?? const [])
        .cast<Map<String, dynamic>>();
    return list.map(SessionInfo.fromJson).toList();
  }

  Future<SessionInfo> createSession({
    String shell = 'auto',
    int cols = 80,
    int rows = 24,
    String? title,
  }) async {
    final res = await _dio.post<Map<String, dynamic>>(
      '/api/sessions',
      data: {
        if (shell.isNotEmpty) 'shell': shell,
        'cols': cols,
        'rows': rows,
        if (title != null && title.isNotEmpty) 'title': title,
      },
    );
    return SessionInfo.fromJson(res.data ?? const {});
  }

  Future<SessionInfo> renameSession(String id, String title) async {
    final res = await _dio.patch<Map<String, dynamic>>(
      '/api/sessions/$id',
      data: {'title': title},
    );
    return SessionInfo.fromJson(res.data ?? const {});
  }

  Future<void> killSession(String id) async {
    await _dio.delete<void>('/api/sessions/$id');
  }

  /// WebSocket URL for a session, including the `?token=<lan_bearer>`
  /// query parameter. Browsers/Flutter cannot set Authorization on the
  /// WS handshake, so the bearer is encoded in the URL itself; this is
  /// honored by the relay's auth middleware as an alternative to the
  /// header.
  Uri wsUrlForSession(String id) {
    final base = Uri.parse(profile.baseUrl);
    final scheme = base.scheme == 'https' ? 'wss' : 'ws';
    final query = <String, String>{};
    if (profile.lanBearer != null && profile.lanBearer!.isNotEmpty) {
      query['token'] = profile.lanBearer!;
    }
    return Uri(
      scheme: scheme,
      host: base.host,
      port: base.port,
      path: '/api/sessions/$id/io',
      queryParameters: query.isEmpty ? null : query,
    );
  }
}
