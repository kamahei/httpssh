import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../l10n/generated/app_localizations.dart';
import '../models/profile.dart';
import '../state/profile_repository.dart';
import 'cloudflare_sso_screen.dart';

class ProfileEditor extends ConsumerStatefulWidget {
  const ProfileEditor({super.key, this.initial});
  final Profile? initial;

  @override
  ConsumerState<ProfileEditor> createState() => _ProfileEditorState();
}

class _ProfileEditorState extends ConsumerState<ProfileEditor> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _name;
  late final TextEditingController _baseUrl;
  late final TextEditingController _bearer;
  late final TextEditingController _cfId;
  late final TextEditingController _cfSecret;
  AuthMode _mode = AuthMode.bearerOnly;
  String? _capturedCookie;

  @override
  void initState() {
    super.initState();
    final p = widget.initial;
    _name = TextEditingController(text: p?.name ?? '');
    _baseUrl = TextEditingController(text: p?.baseUrl ?? '');
    _bearer = TextEditingController(text: p?.lanBearer ?? '');
    _cfId = TextEditingController(text: p?.cfClientId ?? '');
    _cfSecret = TextEditingController(text: p?.cfClientSecret ?? '');
    _mode = p?.authMode ?? AuthMode.bearerOnly;
    _capturedCookie = p?.sessionCookie;
  }

  @override
  void dispose() {
    _name.dispose();
    _baseUrl.dispose();
    _bearer.dispose();
    _cfId.dispose();
    _cfSecret.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context)!;
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.initial == null ? t.profileNew : t.profileEditTitle),
        actions: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 8),
            child: FilledButton.icon(
              onPressed: _save,
              icon: const Icon(Icons.save),
              label: Text(t.profileSave),
            ),
          ),
        ],
      ),
      // A second, full-width Save button anchored to the bottom of the
      // screen ensures the action is reachable even on tiny screens
      // where the AppBar action might be partially clipped.
      bottomNavigationBar: SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
          child: FilledButton.icon(
            onPressed: _save,
            icon: const Icon(Icons.save),
            label: Text(t.profileSave),
            style: FilledButton.styleFrom(
              minimumSize: const Size.fromHeight(48),
            ),
          ),
        ),
      ),
      body: Form(
        key: _formKey,
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            TextFormField(
              controller: _name,
              decoration: InputDecoration(
                labelText: t.profileFieldName,
                hintText: t.profileFieldNameHint,
              ),
              validator: (v) => (v ?? '').isEmpty ? t.errorRequiredField : null,
            ),
            const SizedBox(height: 16),
            TextFormField(
              controller: _baseUrl,
              decoration: InputDecoration(
                labelText: t.profileFieldBaseUrl,
                hintText: t.profileFieldBaseUrlHint,
              ),
              validator: _validateUrl,
              autocorrect: false,
              keyboardType: TextInputType.url,
            ),

            // The bearer is required for every mode and is the relay's
            // primary auth check, so the field lives at the top, not
            // inside the per-mode block.
            const SizedBox(height: 16),
            TextFormField(
              controller: _bearer,
              decoration: InputDecoration(labelText: t.profileFieldLanBearer),
              obscureText: true,
              autocorrect: false,
              validator: (v) =>
                  (v ?? '').length < 16 ? t.errorInvalidBearer : null,
            ),

            const SizedBox(height: 24),
            Text(
              t.profileFieldAuthMode,
              style: Theme.of(context).textTheme.labelLarge,
            ),
            const SizedBox(height: 8),
            SegmentedButton<AuthMode>(
              segments: [
                ButtonSegment(
                  value: AuthMode.bearerOnly,
                  label: Text(t.profileAuthBearerOnly),
                ),
                ButtonSegment(
                  value: AuthMode.bearerPlusServiceToken,
                  label: Text(t.profileAuthBearerPlusServiceToken),
                ),
                ButtonSegment(
                  value: AuthMode.bearerPlusBrowserSso,
                  label: Text(t.profileAuthBearerPlusBrowserSso),
                ),
              ],
              selected: {_mode},
              onSelectionChanged: (s) => setState(() => _mode = s.first),
            ),
            const SizedBox(height: 16),
            Text(
              _modeHint(t),
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: Theme.of(context).colorScheme.outline,
                  ),
            ),
            const SizedBox(height: 16),
            ..._modeFields(t),
          ],
        ),
      ),
    );
  }

  String _modeHint(AppLocalizations t) => switch (_mode) {
        AuthMode.bearerOnly => t.profileAuthBearerOnlyHint,
        AuthMode.bearerPlusServiceToken =>
          t.profileAuthBearerPlusServiceTokenHint,
        AuthMode.bearerPlusBrowserSso => t.profileAuthBearerPlusBrowserSsoHint,
      };

  List<Widget> _modeFields(AppLocalizations t) {
    switch (_mode) {
      case AuthMode.bearerOnly:
        return [];
      case AuthMode.bearerPlusServiceToken:
        return [
          TextFormField(
            controller: _cfId,
            decoration: InputDecoration(labelText: t.profileFieldCfClientId),
            autocorrect: false,
            validator: (v) => (v ?? '').isEmpty ? t.errorRequiredField : null,
          ),
          const SizedBox(height: 16),
          TextFormField(
            controller: _cfSecret,
            decoration:
                InputDecoration(labelText: t.profileFieldCfClientSecret),
            obscureText: true,
            autocorrect: false,
            validator: (v) => (v ?? '').isEmpty ? t.errorRequiredField : null,
          ),
        ];
      case AuthMode.bearerPlusBrowserSso:
        final hasCookie = (_capturedCookie ?? '').isNotEmpty;
        return [
          Card(
            child: Padding(
              padding: const EdgeInsets.all(12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(t.profileAuthBrowserSsoNote),
                  const SizedBox(height: 12),
                  Row(
                    children: [
                      Icon(
                        hasCookie ? Icons.check_circle : Icons.info_outline,
                        size: 20,
                        color: hasCookie
                            ? Colors.green
                            : Theme.of(context).colorScheme.outline,
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(
                          hasCookie
                              ? t.profileSsoCookieCaptured
                              : t.profileSsoCookieMissing,
                          style: Theme.of(context).textTheme.bodySmall,
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 8),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: FilledButton.tonalIcon(
                      onPressed: _signInWithGoogle,
                      icon: const Icon(Icons.login),
                      label: Text(
                        hasCookie
                            ? t.profileSsoLoginRefresh
                            : t.profileSsoLoginNow,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ];
    }
  }

  Future<void> _signInWithGoogle() async {
    final t = AppLocalizations.of(context)!;
    final base = _baseUrl.text.trim();
    if (base.isEmpty || _validateUrl(base) != null) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(t.ssoNeedsBaseUrl)),
      );
      return;
    }
    final cookie = await Navigator.of(context).push<String?>(
      MaterialPageRoute(
        builder: (_) => CloudflareSsoScreen(baseUrl: base),
      ),
    );
    if (!mounted || cookie == null || cookie.isEmpty) return;
    setState(() => _capturedCookie = cookie);
  }

  String? _validateUrl(String? v) {
    final t = AppLocalizations.of(context)!;
    if ((v ?? '').isEmpty) return t.errorRequiredField;
    final uri = Uri.tryParse(v!);
    if (uri == null || (uri.scheme != 'http' && uri.scheme != 'https')) {
      return t.errorInvalidUrl;
    }
    return null;
  }

  Future<void> _save() async {
    if (!_formKey.currentState!.validate()) return;
    final base = widget.initial ??
        Profile.create(
          name: _name.text.trim(),
          baseUrl: _baseUrl.text.trim(),
          authMode: _mode,
        );
    final updated = base.copyWith(
      name: _name.text.trim(),
      baseUrl: _baseUrl.text.trim(),
      authMode: _mode,
      // The bearer field is shown for every mode and persisted always.
      lanBearer: _bearer.text,
      cfClientId: _mode == AuthMode.bearerPlusServiceToken ? _cfId.text : null,
      cfClientSecret:
          _mode == AuthMode.bearerPlusServiceToken ? _cfSecret.text : null,
      sessionCookie:
          _mode == AuthMode.bearerPlusBrowserSso ? _capturedCookie : null,
    );
    await ref.read(profilesProvider.notifier).save(updated);
    if (mounted) Navigator.of(context).pop();
  }
}
