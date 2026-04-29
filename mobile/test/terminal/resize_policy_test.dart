import 'package:flutter_test/flutter_test.dart';
import 'package:httpssh_mobile/terminal/resize_policy.dart';

void main() {
  group('isPowerShellShell', () {
    test('matches logical and absolute PowerShell executable names', () {
      expect(isPowerShellShell('pwsh'), isTrue);
      expect(isPowerShellShell('powershell'), isTrue);
      expect(
        isPowerShellShell(r'C:\Program Files\PowerShell\7\pwsh.exe'),
        isTrue,
      );
      expect(
        isPowerShellShell(
          r'C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe',
        ),
        isTrue,
      );
    });

    test('does not match cmd or unrelated executables', () {
      expect(isPowerShellShell('cmd'), isFalse);
      expect(isPowerShellShell(r'C:\Windows\System32\cmd.exe'), isFalse);
    });
  });

  group('remoteColsFor', () {
    test('keeps PowerShell wrap mode at a wide remote width', () {
      expect(
        remoteColsFor(shell: 'pwsh', lineWrap: true, visibleCols: 54),
        kHorizontalScrollCols,
      );
    });

    test('does not shrink PowerShell below visible width on wide screens', () {
      expect(
        remoteColsFor(shell: 'powershell', lineWrap: true, visibleCols: 160),
        160,
      );
    });

    test('uses visible width for non-PowerShell wrap mode', () {
      expect(
        remoteColsFor(shell: 'cmd', lineWrap: true, visibleCols: 54),
        54,
      );
    });

    test('uses visible width when wrap mode is off', () {
      expect(
        remoteColsFor(shell: 'pwsh', lineWrap: false, visibleCols: 120),
        120,
      );
    });
  });
}
