import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../l10n/generated/app_localizations.dart';
import '../models/profile.dart';
import '../state/profile_repository.dart';
import 'profile_editor.dart';
import 'sessions_screen.dart';
import 'settings_screen.dart';

class ProfilesScreen extends ConsumerWidget {
  const ProfilesScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = AppLocalizations.of(context)!;
    final profilesAsync = ref.watch(profilesProvider);

    return Scaffold(
      appBar: AppBar(
        title: Text(t.appTitle),
        actions: [
          IconButton(
            icon: const Icon(Icons.settings),
            tooltip: t.settingsTitle,
            onPressed: () {
              Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) => const SettingsScreen(),
                ),
              );
            },
          ),
        ],
      ),
      body: profilesAsync.when(
        data: (profiles) {
          if (profiles.isEmpty) {
            return Padding(
              padding: const EdgeInsets.all(32),
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Icon(
                    Icons.cable,
                    size: 72,
                    color: Theme.of(context).colorScheme.outline,
                  ),
                  const SizedBox(height: 16),
                  Text(
                    t.profilesEmptyTitle,
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 8),
                  Text(
                    t.profilesEmptyDescription,
                    style: Theme.of(context).textTheme.bodySmall,
                    textAlign: TextAlign.center,
                  ),
                ],
              ),
            );
          }
          return ListView.separated(
            itemBuilder: (_, i) => _ProfileTile(profile: profiles[i]),
            separatorBuilder: (_, __) => const Divider(height: 1),
            itemCount: profiles.length,
          );
        },
        error: (e, _) => Center(child: Text('$e')),
        loading: () => const Center(child: CircularProgressIndicator()),
      ),
      floatingActionButton: FloatingActionButton(
        tooltip: t.profileAddTooltip,
        onPressed: () => _openEditor(context, null),
        child: const Icon(Icons.add),
      ),
    );
  }

  Future<void> _openEditor(BuildContext context, Profile? existing) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => ProfileEditor(initial: existing),
      ),
    );
  }
}

class _ProfileTile extends ConsumerWidget {
  const _ProfileTile({required this.profile});
  final Profile profile;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = AppLocalizations.of(context)!;
    final mode = switch (profile.authMode) {
      AuthMode.bearerOnly => 'Bearer',
      AuthMode.bearerPlusServiceToken => 'Bearer + CF Token',
      AuthMode.bearerPlusBrowserSso => 'Bearer + CF SSO',
    };
    return ListTile(
      title: Text(profile.name),
      subtitle: Text(profile.baseUrl),
      trailing: Wrap(
        spacing: 8,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: [
          Chip(label: Text(mode)),
          PopupMenuButton<String>(
            onSelected: (v) async {
              switch (v) {
                case 'edit':
                  await Navigator.of(context).push(
                    MaterialPageRoute<void>(
                      builder: (_) => ProfileEditor(initial: profile),
                    ),
                  );
                case 'delete':
                  final ok = await showDialog<bool>(
                    context: context,
                    builder: (ctx) => AlertDialog(
                      content: Text(t.profileDeleteConfirm(profile.name)),
                      actions: [
                        TextButton(
                          onPressed: () => Navigator.pop(ctx, false),
                          child: Text(t.cancel),
                        ),
                        TextButton(
                          onPressed: () => Navigator.pop(ctx, true),
                          child: Text(t.profileDelete),
                        ),
                      ],
                    ),
                  );
                  if (ok ?? false) {
                    await ref
                        .read(profilesProvider.notifier)
                        .delete(profile.id);
                  }
              }
            },
            itemBuilder: (_) => [
              PopupMenuItem(value: 'edit', child: Text(t.profileEdit)),
              PopupMenuItem(value: 'delete', child: Text(t.profileDelete)),
            ],
          ),
        ],
      ),
      onTap: () {
        Navigator.of(context).push(
          MaterialPageRoute<void>(
            builder: (_) => SessionsScreen(profile: profile),
          ),
        );
      },
    );
  }
}
