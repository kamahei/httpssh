import 'package:uuid/uuid.dart';

/// How the request reaches the relay. The LAN bearer is required in
/// every mode; this enum only describes the OUTER (Cloudflare-edge)
/// layer added on top.
enum AuthMode {
  /// LAN bearer only. Used either for direct LAN connections or for
  /// browser access through Cloudflare Tunnel where Cloudflare Access
  /// handles identity at the edge (Google SSO) and the relay just
  /// needs the bearer.
  bearerOnly,

  /// LAN bearer plus a Cloudflare Service Token (`CF-Access-Client-Id`
  /// and `CF-Access-Client-Secret`). The mobile app's primary mode for
  /// reaching a Cloudflare-fronted relay non-interactively.
  bearerPlusServiceToken,

  /// LAN bearer plus a Cloudflare Access SSO session cookie obtained by
  /// the user logging into Google through an in-app browser. Used by
  /// the mobile app when Service Tokens are not desired.
  bearerPlusBrowserSso,
}

/// Connection profile persisted on the device. The LAN bearer is
/// mandatory regardless of mode. Cloudflare-side credentials
/// (`cfClientId`/`cfClientSecret` for Service Token mode, or
/// `sessionCookie` for browser SSO mode) are additive and stored
/// separately in `flutter_secure_storage` keyed by `id`; this model
/// holds them only in memory.
class Profile {
  Profile({
    required this.id,
    required this.name,
    required this.baseUrl,
    required this.authMode,
    this.lanBearer,
    this.cfClientId,
    this.cfClientSecret,
    this.sessionCookie,
    DateTime? createdAt,
  }) : createdAt = createdAt ?? DateTime.now();

  factory Profile.create({
    required String name,
    required String baseUrl,
    required AuthMode authMode,
  }) {
    return Profile(
      id: const Uuid().v4(),
      name: name,
      baseUrl: baseUrl,
      authMode: authMode,
    );
  }

  final String id;
  final String name;
  final String baseUrl;
  final AuthMode authMode;
  final String? lanBearer;
  final String? cfClientId;
  final String? cfClientSecret;
  final String? sessionCookie;
  final DateTime createdAt;

  Profile copyWith({
    String? name,
    String? baseUrl,
    AuthMode? authMode,
    String? lanBearer,
    String? cfClientId,
    String? cfClientSecret,
    String? sessionCookie,
  }) {
    return Profile(
      id: id,
      name: name ?? this.name,
      baseUrl: baseUrl ?? this.baseUrl,
      authMode: authMode ?? this.authMode,
      lanBearer: lanBearer ?? this.lanBearer,
      cfClientId: cfClientId ?? this.cfClientId,
      cfClientSecret: cfClientSecret ?? this.cfClientSecret,
      sessionCookie: sessionCookie ?? this.sessionCookie,
      createdAt: createdAt,
    );
  }

  /// Non-secret JSON used for fast list rendering. Secrets live in the
  /// secure store and are merged in by the repository layer.
  Map<String, dynamic> toMetadataJson() => {
        'id': id,
        'name': name,
        'baseUrl': baseUrl,
        'authMode': authMode.name,
        'createdAt': createdAt.toIso8601String(),
      };

  static Profile fromMetadataJson(Map<String, dynamic> json) {
    final modeName = json['authMode'] as String?;
    // Map legacy enum names from older builds (lanBearer / cfServiceToken
    // / cfBrowserCookie) to the new bearerOnly / bearerPlus* values so
    // that profiles created before this refactor keep working.
    final mode = AuthMode.values.firstWhere(
      (m) => m.name == modeName,
      orElse: () => switch (modeName) {
        'lanBearer' => AuthMode.bearerOnly,
        'cfServiceToken' => AuthMode.bearerPlusServiceToken,
        'cfBrowserCookie' => AuthMode.bearerPlusBrowserSso,
        _ => AuthMode.bearerOnly,
      },
    );
    return Profile(
      id: json['id'] as String,
      name: json['name'] as String,
      baseUrl: json['baseUrl'] as String,
      authMode: mode,
      createdAt: DateTime.tryParse(json['createdAt'] as String? ?? '') ??
          DateTime.now(),
    );
  }
}
