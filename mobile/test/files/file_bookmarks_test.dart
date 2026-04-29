import 'package:flutter_test/flutter_test.dart';
import 'package:httpssh_mobile/files/file_bookmarks.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('persists bookmarks per profile', () async {
    const store = FileBookmarkStore();
    const bookmark = FileBookmark(
      rootId: 'main',
      path: 'src',
      label: 'Source',
    );

    await store.add('profile-a', bookmark);

    expect(await store.load('profile-a'), hasLength(1));
    expect(await store.load('profile-b'), isEmpty);
  });

  test('deduplicates by target', () async {
    const store = FileBookmarkStore();
    await store.add(
      'profile',
      const FileBookmark(rootId: 'main', path: 'src', label: 'Old'),
    );
    final next = await store.add(
      'profile',
      const FileBookmark(rootId: 'main', path: 'src', label: 'New'),
    );

    expect(next, hasLength(1));
    expect(next.single.label, 'New');
  });

  test('removes matching target', () async {
    const store = FileBookmarkStore();
    const bookmark = FileBookmark(rootId: 'main', path: 'src', label: 'Source');
    await store.add('profile', bookmark);

    final next = await store.remove('profile', bookmark);

    expect(next, isEmpty);
  });
}
