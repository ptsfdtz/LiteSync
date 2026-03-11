//go:build windows

package folderpicker

import (
	"os/exec"
	"strings"
)

func pick(initialPath string) (string, error) {
	script := buildPowerShellScript(initialPath)

	command := exec.Command(
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-STA",
		"-ExecutionPolicy",
		"Bypass",
		"-WindowStyle",
		"Hidden",
		"-Command",
		script,
	)

	output, err := command.Output()
	if err != nil {
		return "", err
	}

	selectedPath := strings.TrimSpace(string(output))
	if selectedPath == "" {
		return "", ErrCancelled
	}

	return selectedPath, nil
}

func buildPowerShellScript(initialPath string) string {
	escapedPath := strings.ReplaceAll(initialPath, "'", "''")

	return strings.Join([]string{
		"Add-Type -AssemblyName System.Windows.Forms;",
		"$dialog = New-Object System.Windows.Forms.FolderBrowserDialog;",
		"$dialog.Description = '请选择文件夹';",
		"$dialog.ShowNewFolderButton = $true;",
		"$dialog.UseDescriptionForTitle = $true;",
		"if ('" + escapedPath + "' -ne '') { $dialog.SelectedPath = '" + escapedPath + "' }",
		"$result = $dialog.ShowDialog();",
		"if ($result -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $dialog.SelectedPath }",
	}, " ")
}
