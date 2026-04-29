import 'package:flutter_test/flutter_test.dart';
import 'package:httpssh_mobile/terminal/terminal_input.dart';

void main() {
  group('terminalInputFromEditorText', () {
    test('converts editor line feeds to terminal carriage returns', () {
      expect(
        terminalInputFromEditorText('line 1\nline 2', appendEnter: false),
        'line 1\rline 2',
      );
    });

    test('normalizes Windows line endings', () {
      expect(
        terminalInputFromEditorText('line 1\r\nline 2', appendEnter: false),
        'line 1\rline 2',
      );
    });

    test('appends Enter when requested', () {
      expect(
        terminalInputFromEditorText('Get-Date', appendEnter: true),
        'Get-Date\r',
      );
    });

    test('does not append duplicate Enter', () {
      expect(
        terminalInputFromEditorText('Get-Date\n', appendEnter: true),
        'Get-Date\r',
      );
    });
  });
}
