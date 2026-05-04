import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Thin wrapper around [FlutterSecureStorage] that namespaces secrets per
/// profile id. Keys take the form `profile.<id>.<field>`.
class SecureStore {
  SecureStore([FlutterSecureStorage? storage])
      : _storage = storage ?? const FlutterSecureStorage();

  final FlutterSecureStorage _storage;

  String _key(String id, String field) => 'profile.$id.$field';

  Future<void> writeBearer(String profileId, String value) =>
      _storage.write(key: _key(profileId, 'lanBearer'), value: value);
  Future<String?> readBearer(String profileId) =>
      _storage.read(key: _key(profileId, 'lanBearer'));

  Future<void> writeServiceToken(
    String profileId,
    String clientId,
    String clientSecret,
  ) async {
    await _storage.write(key: _key(profileId, 'cfClientId'), value: clientId);
    await _storage.write(
      key: _key(profileId, 'cfClientSecret'),
      value: clientSecret,
    );
  }

  Future<({String? id, String? secret})> readServiceToken(
    String profileId,
  ) async {
    final id = await _storage.read(key: _key(profileId, 'cfClientId'));
    final secret = await _storage.read(key: _key(profileId, 'cfClientSecret'));
    return (id: id, secret: secret);
  }

  Future<void> writeSessionCookie(String profileId, String cookie) =>
      _storage.write(key: _key(profileId, 'sessionCookie'), value: cookie);
  Future<String?> readSessionCookie(String profileId) =>
      _storage.read(key: _key(profileId, 'sessionCookie'));

  Future<void> deleteAllForProfile(String profileId) async {
    for (final f in const [
      'lanBearer',
      'cfClientId',
      'cfClientSecret',
      'sessionCookie',
    ]) {
      await _storage.delete(key: _key(profileId, f));
    }
  }
}
