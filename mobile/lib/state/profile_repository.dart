import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../auth/secure_store.dart';
import '../models/profile.dart';

/// Persistent profile storage that splits non-secret metadata into shared
/// preferences and secret credentials into FlutterSecureStorage.
///
/// The LAN bearer is read and written for every profile regardless of
/// auth mode; Cloudflare-side credentials are layered on top per mode.
class ProfileRepository {
  ProfileRepository(this._prefs, this._secure);

  static const _kProfilesKey = 'profiles.metadata.v1';
  final SharedPreferences _prefs;
  final SecureStore _secure;

  Future<List<Profile>> loadAll() async {
    final raw = _prefs.getString(_kProfilesKey);
    if (raw == null || raw.isEmpty) return [];
    final list = (jsonDecode(raw) as List).cast<Map<String, dynamic>>();
    final profiles = <Profile>[];
    for (final j in list) {
      final base = Profile.fromMetadataJson(j);
      profiles.add(await _hydrate(base));
    }
    return profiles;
  }

  Future<Profile> _hydrate(Profile p) async {
    final bearer = await _secure.readBearer(p.id);
    Profile out = p.copyWith(lanBearer: bearer);
    switch (p.authMode) {
      case AuthMode.bearerOnly:
        return out;
      case AuthMode.bearerPlusServiceToken:
        final t = await _secure.readServiceToken(p.id);
        return out.copyWith(cfClientId: t.id, cfClientSecret: t.secret);
      case AuthMode.bearerPlusBrowserSso:
        final c = await _secure.readSessionCookie(p.id);
        return out.copyWith(sessionCookie: c);
    }
  }

  Future<void> save(Profile p) async {
    final all = await loadAll();
    final idx = all.indexWhere((x) => x.id == p.id);
    if (idx == -1) {
      all.add(p);
    } else {
      all[idx] = p;
    }
    await _persistMetadata(all);
    await _persistSecrets(p);
  }

  Future<void> delete(String id) async {
    final all = await loadAll();
    all.removeWhere((p) => p.id == id);
    await _persistMetadata(all);
    await _secure.deleteAllForProfile(id);
  }

  Future<void> _persistMetadata(List<Profile> all) async {
    final list = all.map((p) => p.toMetadataJson()).toList();
    await _prefs.setString(_kProfilesKey, jsonEncode(list));
  }

  Future<void> _persistSecrets(Profile p) async {
    if (p.lanBearer != null && p.lanBearer!.isNotEmpty) {
      await _secure.writeBearer(p.id, p.lanBearer!);
    }
    switch (p.authMode) {
      case AuthMode.bearerOnly:
        break;
      case AuthMode.bearerPlusServiceToken:
        final id = p.cfClientId ?? '';
        final secret = p.cfClientSecret ?? '';
        if (id.isNotEmpty && secret.isNotEmpty) {
          await _secure.writeServiceToken(p.id, id, secret);
        }
      case AuthMode.bearerPlusBrowserSso:
        if (p.sessionCookie != null && p.sessionCookie!.isNotEmpty) {
          await _secure.writeSessionCookie(p.id, p.sessionCookie!);
        }
    }
  }
}

final profileRepositoryProvider =
    FutureProvider<ProfileRepository>((ref) async {
  final prefs = await SharedPreferences.getInstance();
  return ProfileRepository(prefs, SecureStore());
});

class ProfilesNotifier extends AsyncNotifier<List<Profile>> {
  late ProfileRepository _repo;

  @override
  Future<List<Profile>> build() async {
    _repo = await ref.read(profileRepositoryProvider.future);
    return _repo.loadAll();
  }

  Future<void> save(Profile p) async {
    await _repo.save(p);
    state = AsyncData(await _repo.loadAll());
  }

  Future<void> delete(String id) async {
    await _repo.delete(id);
    state = AsyncData(await _repo.loadAll());
  }
}

final profilesProvider = AsyncNotifierProvider<ProfilesNotifier, List<Profile>>(
    ProfilesNotifier.new,);
