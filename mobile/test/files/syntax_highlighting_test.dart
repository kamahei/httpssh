import 'package:flutter/painting.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_highlighting/themes/github.dart';
import 'package:httpssh_mobile/files/syntax_highlighting.dart';

void main() {
  group('highlightLanguageForPath', () {
    test('detects common code and config file types', () {
      expect(highlightLanguageForPath('script.ps1'), 'powershell');
      expect(highlightLanguageForPath('data.json'), 'json');
      expect(highlightLanguageForPath('config.yaml'), 'yaml');
      expect(highlightLanguageForPath('README.md'), 'markdown');
      expect(highlightLanguageForPath('main.dart'), 'dart');
      expect(highlightLanguageForPath('server.go'), 'go');
      expect(highlightLanguageForPath('tool.py'), 'python');
      expect(highlightLanguageForPath('lib.rs'), 'rust');
      expect(highlightLanguageForPath('query.sql'), 'sql');
      expect(highlightLanguageForPath('Dockerfile'), 'dockerfile');
      expect(highlightLanguageForPath('Makefile'), 'makefile');
    });

    test('returns null for unknown extensions', () {
      expect(highlightLanguageForPath('notes.unknown'), isNull);
      expect(highlightLanguageForPath('README'), isNull);
    });
  });

  group('syntaxHighlightSpans', () {
    test('creates highlighted spans for a detected language', () {
      final spans = syntaxHighlightSpans(
        content: 'Write-Host "hello"',
        language: 'powershell',
        theme: githubTheme,
        baseStyle: const TextStyle(fontFamily: 'monospace'),
      );

      expect(spans, isNotEmpty);
    });
  });
}
