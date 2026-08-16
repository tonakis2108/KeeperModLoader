//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type gameListModel struct {
	walk.ListModelBase
	items []*GameInfo
}

func (m *gameListModel) ItemCount() int { return len(m.items) }
func (m *gameListModel) Value(index int) interface{} {
	if index < 0 || index >= len(m.items) {
		return ""
	}
	return m.items[index].String()
}
func (m *gameListModel) reset(items []*GameInfo) { m.items = items; m.PublishItemsReset() }

type modListModel struct {
	walk.ListModelBase
	items []*InstalledMod
}

func (m *modListModel) ItemCount() int { return len(m.items) }
func (m *modListModel) Value(index int) interface{} {
	if index < 0 || index >= len(m.items) {
		return ""
	}
	return m.items[index].String()
}
func (m *modListModel) reset(items []*InstalledMod) { m.items = items; m.PublishItemsReset() }

type application struct {
	mw      *walk.MainWindow
	list    *walk.ListBox
	log     *walk.TextEdit
	status  *walk.Label
	buttons []*walk.PushButton
	model   *gameListModel
}

func main() {
	if handleManagerUpdateMode() {
		return
	}
	app := &application{model: &gameListModel{}}
	var scanButton, addButton, managerUpdateButton, enableButton, disableButton, manageButton, savesButton *walk.PushButton
	window := MainWindow{
		AssignTo: &app.mw,
		Title:    "KeeperLoader Universal Manager " + loaderVersion,
		MinSize:  Size{Width: 850, Height: 580},
		Size:     Size{Width: 980, Height: 650},
		Layout:   VBox{MarginsZero: false, Spacing: 8},
		Children: []Widget{
			Label{Text: "KeeperLoader Universal Manager", Font: Font{Family: "Segoe UI", PointSize: 16, Bold: true}},
			Label{Text: "Native Windows manager for compatible Unity Mono games"},
			ListBox{AssignTo: &app.list, Model: app.model, MultiSelection: true, MinSize: Size{Height: 290}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 7}, Children: []Widget{
				PushButton{AssignTo: &scanButton, Text: "Scan Steam", OnClicked: func() { app.scanSteam() }},
				PushButton{AssignTo: &addButton, Text: "Add game…", OnClicked: func() { app.addGame() }},
				PushButton{AssignTo: &managerUpdateButton, Text: "Install manager update…", OnClicked: func() { app.installManagerUpdate() }},
				HSpacer{},
			}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 7}, Children: []Widget{
				HSpacer{},
				PushButton{AssignTo: &enableButton, Text: "Enable / update selected", OnClicked: func() { app.enableSelected() }},
				PushButton{AssignTo: &disableButton, Text: "Remove selected", OnClicked: func() { app.disableSelected() }},
				PushButton{AssignTo: &manageButton, Text: "Manage mods…", OnClicked: func() { app.manageSelected() }},
				PushButton{AssignTo: &savesButton, Text: "Open saves", OnClicked: func() { app.openSaves() }},
			}},
			Label{Text: "Activity"},
			TextEdit{AssignTo: &app.log, ReadOnly: true, VScroll: true, MinSize: Size{Height: 115}, Text: "Ready."},
			Label{AssignTo: &app.status, Text: "Select one or more games. Ctrl-click selects multiple entries."},
		},
	}
	if err := window.Create(); err != nil {
		walk.MsgBox(nil, "KeeperLoader", err.Error(), walk.MsgBoxIconError)
		return
	}
	app.buttons = []*walk.PushButton{scanButton, addButton, managerUpdateButton, enableButton, disableButton, manageButton, savesButton}
	if remembered, rememberErr := loadRememberedGames(); rememberErr != nil {
		app.appendLog("Could not load remembered game locations: " + rememberErr.Error())
	} else {
		app.model.reset(remembered)
	}
	app.mw.Starting().Once(func() { app.scanSteam() })
	app.mw.Run()
}

func (app *application) appendLog(message string) {
	app.log.AppendText("\r\n" + time.Now().Format("15:04:05") + "  " + message)
}

func (app *application) setBusy(busy bool, message string) {
	for _, button := range app.buttons {
		button.SetEnabled(!busy)
	}
	app.list.SetEnabled(!busy)
	app.status.SetText(message)
}

func (app *application) selectedGames(requireOne bool) []*GameInfo {
	indexes := app.list.SelectedIndexes()
	if len(indexes) == 0 {
		walk.MsgBox(app.mw, "No game selected", "Select at least one game first.", walk.MsgBoxIconInformation)
		return nil
	}
	if requireOne && len(indexes) != 1 {
		walk.MsgBox(app.mw, "Select one game", "Select exactly one game for this operation.", walk.MsgBoxIconInformation)
		return nil
	}
	result := make([]*GameInfo, 0, len(indexes))
	for _, index := range indexes {
		if index >= 0 && index < len(app.model.items) {
			result = append(result, app.model.items[index])
		}
	}
	return result
}

func mergeGames(existing, added []*GameInfo) []*GameInfo {
	seen := map[string]bool{}
	var result []*GameInfo
	for _, list := range [][]*GameInfo{existing, added} {
		for _, game := range list {
			key := strings.ToLower(game.GameDirectory)
			if !seen[key] {
				seen[key] = true
				result = append(result, game)
			}
		}
	}
	return result
}

func (app *application) scanSteam() {
	app.setBusy(true, "Scanning Steam libraries…")
	go func() {
		games, err := scanSteamGames()
		app.mw.Synchronize(func() {
			defer app.setBusy(false, "Ready.")
			if err != nil {
				app.appendLog("Scan failed: " + err.Error())
				return
			}
			app.model.reset(mergeGames(app.model.items, games))
			if saveErr := saveRememberedGames(app.model.items); saveErr != nil {
				app.appendLog("Could not remember game locations: " + saveErr.Error())
			}
			app.appendLog(fmt.Sprintf("Steam scan found %d compatible game(s).", len(games)))
		})
	}()
}

func (app *application) addGame() {
	dialog := new(walk.FileDialog)
	dialog.Title = "Select a Windows Unity game folder"
	accepted, err := dialog.ShowBrowseFolder(app.mw)
	if err != nil {
		walk.MsgBox(app.mw, "Folder selection failed", err.Error(), walk.MsgBoxIconError)
		return
	}
	if !accepted {
		return
	}
	game, err := detectUnityGame(dialog.FilePath)
	if err != nil {
		walk.MsgBox(app.mw, "Game not detected", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	if !game.Supported {
		walk.MsgBox(app.mw, "Unsupported Unity game", game.Reason, walk.MsgBoxIconWarning)
		return
	}
	app.model.reset(mergeGames(app.model.items, []*GameInfo{game}))
	if err = saveRememberedGames(app.model.items); err != nil {
		app.appendLog("Could not remember game locations: " + err.Error())
	}
	app.appendLog("Added " + game.ProcessName + ".")
}

func (app *application) enableSelected() {
	games := app.selectedGames(false)
	if len(games) == 0 {
		return
	}
	app.setBusy(true, "Enabling or updating KeeperLoader…")
	go func() {
		for _, game := range games {
			backup, err := enableLoader(game)
			app.mw.Synchronize(func() {
				if err != nil {
					app.appendLog("Could not enable " + game.ProcessName + ": " + err.Error())
				} else {
					app.appendLog("Enabled/updated " + game.ProcessName + " to KeeperLoader " + loaderVersion + ". Original bootstrap backup: " + backup)
				}
			})
		}
		app.mw.Synchronize(func() { app.model.PublishItemsReset(); app.setBusy(false, "Enable/update operation completed.") })
	}()
}

func (app *application) disableSelected() {
	games := app.selectedGames(false)
	if len(games) == 0 {
		return
	}
	if walk.MsgBox(app.mw, "Confirm complete removal", fmt.Sprintf("Permanently remove KeeperLoader from %d selected game(s)?\r\n\r\nThis deletes only KeeperLoader's game-folder files: the loader core, installed mods, configuration, state, logs, backups, and KeeperLoader bootstrap files. Original pre-loader bootstrap files are restored when available. The game installation and game saves are not touched.\r\n\r\nThis cannot be undone.", len(games)), walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
		return
	}
	app.setBusy(true, "Removing KeeperLoader…")
	go func() {
		for _, game := range games {
			message, err := disableLoader(game)
			app.mw.Synchronize(func() {
				if err != nil {
					app.appendLog("Could not remove KeeperLoader from " + game.ProcessName + ": " + err.Error())
				} else {
					app.appendLog(game.ProcessName + ": " + message)
				}
			})
		}
		app.mw.Synchronize(func() { app.model.PublishItemsReset(); app.setBusy(false, "Removal operation completed.") })
	}()
}

func (app *application) manageSelected() {
	games := app.selectedGames(true)
	if len(games) != 1 {
		return
	}
	if !loaderEnabled(games[0]) {
		walk.MsgBox(app.mw, "KeeperLoader inactive", "Enable KeeperLoader for this game first.", walk.MsgBoxIconInformation)
		return
	}
	showModManager(app.mw, games[0])
	app.model.PublishItemsReset()
}

func (app *application) openSaves() {
	games := app.selectedGames(true)
	if len(games) != 1 {
		return
	}
	path, message, err := persistentDataLocation(games[0])
	if err == nil {
		err = openExplorer(path)
	}
	if err != nil {
		walk.MsgBox(app.mw, "Save-data location", err.Error(), walk.MsgBoxIconError)
		return
	}
	app.appendLog(message + " " + path)
}

func (app *application) installManagerUpdate() {
	dialog := &walk.FileDialog{
		Title:  "Select KeeperLoader Windows artifact ZIP",
		Filter: "KeeperLoader Windows artifact (*.zip)|*.zip",
	}
	accepted, err := dialog.ShowOpen(app.mw)
	if err != nil || !accepted {
		if err != nil {
			walk.MsgBox(app.mw, "Manager update", err.Error(), walk.MsgBoxIconError)
		}
		return
	}
	app.setBusy(true, "Validating manager update…")
	newVersion, err := stageManagerUpdate(dialog.FilePath)
	if err != nil {
		app.setBusy(false, "Manager update rejected.")
		walk.MsgBox(app.mw, "Manager update rejected", err.Error(), walk.MsgBoxIconError)
		return
	}
	walk.MsgBox(app.mw, "Manager update ready", "KeeperLoader "+newVersion+" passed validation. The manager will close, replace itself, and restart. Installed games, mods, settings, state, backups, and saves will not be changed.", walk.MsgBoxIconInformation)
	app.mw.Close()
}

func showModManager(owner walk.Form, game *GameInfo) {
	model := &modListModel{}
	mods, _ := installedMods(game)
	model.reset(mods)
	var dlg *walk.Dialog
	var list *walk.ListBox
	var status *walk.Label
	var installButton, externalButton, toggleButton, updateButton, restoreButton, uninstallButton, safeModeButton, packageButton, openButton, closeButton *walk.PushButton
	refresh := func() { items, _ := installedMods(game); model.reset(items) }
	decl := Dialog{
		AssignTo: &dlg, Title: "KeeperLoader Mods — " + game.ProcessName, FixedSize: false,
		Size: Size{Width: 900, Height: 540}, MinSize: Size{Width: 720, Height: 440},
		Layout: VBox{Spacing: 8},
		Children: []Widget{
			Label{Text: fmt.Sprintf("%s  |  game id: %s  |  Unity %s %s", game.ExecutableName, game.GameID, game.Backend, game.Architecture)},
			ListBox{AssignTo: &list, Model: model, MinSize: Size{Height: 260}},
			Label{Text: "Disable is reversible and preserves mod data. Uninstall permanently deletes that mod's files and data."},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 7}, Children: []Widget{
				PushButton{AssignTo: &installButton, Text: "Install Mod ZIP…", OnClicked: func() {
					fileDialog := &walk.FileDialog{Title: "Select a KeeperLoader mod package", Filter: "KeeperLoader mod package (*.zip)|*.zip"}
					accepted, err := fileDialog.ShowOpen(dlg)
					if err != nil || !accepted {
						if err != nil {
							status.SetText(err.Error())
						}
						return
					}
					status.SetText("Validating and installing package…")
					dlg.SetEnabled(false)
					mod, backup, installErr := installModPackage(game, fileDialog.FilePath)
					dlg.SetEnabled(true)
					if installErr != nil {
						status.SetText(installErr.Error())
						walk.MsgBox(dlg, "Mod package rejected", installErr.Error(), walk.MsgBoxIconError)
						return
					}
					refresh()
					message := fmt.Sprintf("%s %s installed.", mod.Name, mod.Version)
					if backup != "" {
						message += " Previous version backed up."
					}
					status.SetText(message)
				}},
				PushButton{AssignTo: &externalButton, Text: "Install External Plugin ZIP…", OnClicked: func() {
					if walk.MsgBox(dlg, "Experimental external plugin", "KeeperLoader will inspect this ZIP and attempt to attach compatible Unity plugin components. Required third-party runtime dependencies are not supplied, and many packages may remain incompatible.\r\n\r\nOnly continue with code from a source you trust.", walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
						return
					}
					fileDialog := &walk.FileDialog{Title: "Select an external Unity plugin package", Filter: "External plugin package (*.zip)|*.zip"}
					accepted, err := fileDialog.ShowOpen(dlg)
					if err != nil || !accepted {
						if err != nil {
							status.SetText(err.Error())
						}
						return
					}
					status.SetText("Inspecting and installing external plugin…")
					dlg.SetEnabled(false)
					mod, backup, installErr := installExternalPluginPackage(game, fileDialog.FilePath)
					dlg.SetEnabled(true)
					if installErr != nil {
						status.SetText(installErr.Error())
						walk.MsgBox(dlg, "External plugin rejected", installErr.Error(), walk.MsgBoxIconError)
						return
					}
					refresh()
					message := fmt.Sprintf("%s %s installed for an experimental load attempt on the next game start.", mod.Name, mod.Version)
					if backup != "" {
						message += " Previous package backed up."
					}
					status.SetText(message)
				}},
				PushButton{AssignTo: &toggleButton, Text: "Enable / disable selected", OnClicked: func() {
					index := list.CurrentIndex()
					if index < 0 || index >= len(model.items) {
						walk.MsgBox(dlg, "No mod selected", "Select one installed mod first.", walk.MsgBoxIconInformation)
						return
					}
					mod := model.items[index]
					message, toggleErr := setModEnabled(game, mod, !mod.Enabled)
					if toggleErr != nil {
						walk.MsgBox(dlg, "Mod status change failed", toggleErr.Error(), walk.MsgBoxIconError)
						return
					}
					refresh()
					status.SetText(message)
				}},
			}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 7}, Children: []Widget{
				PushButton{AssignTo: &updateButton, Text: "Update selected from ZIP…", OnClicked: func() {
					index := list.CurrentIndex()
					if index < 0 || index >= len(model.items) {
						walk.MsgBox(dlg, "No mod selected", "Select one installed mod first.", walk.MsgBoxIconInformation)
						return
					}
					current := model.items[index]
					title := "Select the newer package for " + current.Name
					filter := "KeeperLoader mod package (*.zip)|*.zip"
					if current.Mode == externalPluginMode {
						title = "Select a replacement external package for " + current.Name
						filter = "External plugin package (*.zip)|*.zip"
					}
					fileDialog := &walk.FileDialog{Title: title, Filter: filter}
					accepted, err := fileDialog.ShowOpen(dlg)
					if err != nil || !accepted {
						if err != nil {
							status.SetText(err.Error())
						}
						return
					}
					status.SetText("Validating mod update…")
					dlg.SetEnabled(false)
					var updated *InstalledMod
					var backup string
					var updateErr error
					if current.Mode == externalPluginMode {
						updated, backup, updateErr = updateExternalPluginPackage(game, current, fileDialog.FilePath)
					} else {
						updated, backup, updateErr = updateModPackage(game, current, fileDialog.FilePath)
					}
					dlg.SetEnabled(true)
					if updateErr != nil {
						status.SetText(updateErr.Error())
						walk.MsgBox(dlg, "Mod update rejected", updateErr.Error(), walk.MsgBoxIconError)
						return
					}
					refresh()
					status.SetText(fmt.Sprintf("%s updated from %s to %s. Previous version: %s", updated.Name, current.Version, updated.Version, backup))
				}},
				PushButton{AssignTo: &restoreButton, Text: "Restore previous", OnClicked: func() {
					index := list.CurrentIndex()
					if index < 0 || index >= len(model.items) {
						walk.MsgBox(dlg, "No mod selected", "Select one installed mod first.", walk.MsgBoxIconInformation)
						return
					}
					current := model.items[index]
					if walk.MsgBox(dlg, "Restore previous mod version", "Restore the most recent backup of "+current.Name+"?\r\n\r\nConfiguration, state, and saves remain untouched.", walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
						return
					}
					restored, currentBackup, restoreErr := restorePreviousMod(game, current)
					if restoreErr != nil {
						walk.MsgBox(dlg, "Restore failed", restoreErr.Error(), walk.MsgBoxIconError)
						return
					}
					refresh()
					status.SetText(fmt.Sprintf("Restored %s %s. Replaced version backed up at %s", restored.Name, restored.Version, currentBackup))
				}},
				PushButton{AssignTo: &uninstallButton, Text: "Uninstall selected", OnClicked: func() {
					index := list.CurrentIndex()
					if index < 0 || index >= len(model.items) {
						walk.MsgBox(dlg, "No mod selected", "Select one installed mod first.", walk.MsgBoxIconInformation)
						return
					}
					mod := model.items[index]
					if walk.MsgBox(dlg, "Confirm permanent uninstall", "Permanently uninstall "+mod.Name+"?\r\n\r\nThe installed mod, its configuration, state, and backups will be deleted. Game saves are not touched.\r\n\r\nThis cannot be undone.", walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
						return
					}
					message, err := uninstallMod(game, mod)
					if err != nil {
						walk.MsgBox(dlg, "Uninstall failed", err.Error(), walk.MsgBoxIconError)
						return
					}
					refresh()
					status.SetText(message)
				}},
			}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 7}, Children: []Widget{
				PushButton{AssignTo: &safeModeButton, Text: safeModeButtonText(game), OnClicked: func() {
					requested := !safeModeNextLaunchRequested(game)
					message, safeModeErr := setSafeModeNextLaunch(game, requested)
					if safeModeErr != nil {
						walk.MsgBox(dlg, "Safe mode", safeModeErr.Error(), walk.MsgBoxIconError)
						return
					}
					safeModeButton.SetText(safeModeButtonText(game))
					status.SetText(message)
				}},
				PushButton{AssignTo: &packageButton, Text: "Build Mod ZIP…", OnClicked: func() { showPackageBuilder(dlg, game) }},
				PushButton{AssignTo: &openButton, Text: "Open mods folder", OnClicked: func() { _ = openExplorer(filepath.Join(game.GameDirectory, "KeeperLoader", "mods")) }},
				HSpacer{}, PushButton{AssignTo: &closeButton, Text: "Close", OnClicked: func() { dlg.Accept() }},
			}},
			Label{AssignTo: &status, Text: "Ready."},
		},
	}
	_, err := decl.Run(owner)
	_ = installButton
	_ = externalButton
	_ = toggleButton
	_ = updateButton
	_ = restoreButton
	_ = uninstallButton
	_ = safeModeButton
	_ = packageButton
	_ = openButton
	_ = closeButton
	if err != nil {
		walk.MsgBox(owner, "Mod manager", err.Error(), walk.MsgBoxIconError)
	}
}

func safeModeButtonText(game *GameInfo) string {
	if safeModeNextLaunchRequested(game) {
		return "Cancel safe mode request"
	}
	return "Safe mode next launch"
}

func showPackageBuilder(owner walk.Form, game *GameInfo) {
	var dlg *walk.Dialog
	var sourceEdit, idEdit, nameEdit, versionEdit, gamesEdit, minimumEdit, outputEdit *walk.LineEdit
	var createButton, cancelButton *walk.PushButton
	browseSource := func() {
		d := &walk.FileDialog{Title: "Select compiled mod folder"}
		accepted, err := d.ShowBrowseFolder(dlg)
		if err == nil && accepted {
			sourceEdit.SetText(d.FilePath)
		}
	}
	browseOutput := func() {
		d := &walk.FileDialog{Title: "Save KeeperLoader mod package", Filter: "KeeperLoader mod package (*.zip)|*.zip", FilePath: idEdit.Text() + "-" + versionEdit.Text() + ".zip"}
		accepted, err := d.ShowSave(dlg)
		if err == nil && accepted {
			outputEdit.SetText(d.FilePath)
		}
	}
	decl := Dialog{
		AssignTo: &dlg, Title: "Build validated KeeperLoader Mod ZIP", FixedSize: true, Size: Size{Width: 670, Height: 410},
		Layout: VBox{Spacing: 8}, Children: []Widget{
			Composite{Layout: Grid{Columns: 3, Spacing: 7}, Children: []Widget{
				Label{Text: "Compiled mod folder"}, LineEdit{AssignTo: &sourceEdit}, PushButton{Text: "Browse…", OnClicked: browseSource},
				Label{Text: "Mod ID"}, LineEdit{AssignTo: &idEdit}, Label{Text: "letters, numbers, . _ -"},
				Label{Text: "Mod name"}, LineEdit{AssignTo: &nameEdit}, Label{},
				Label{Text: "Version"}, LineEdit{AssignTo: &versionEdit, Text: "1.0.0"}, Label{},
				Label{Text: "Supported game IDs"}, LineEdit{AssignTo: &gamesEdit, Text: game.GameID}, Label{Text: "explicit IDs, comma-separated"},
				Label{Text: "Minimum loader"}, LineEdit{AssignTo: &minimumEdit, Text: "0.3.0"}, Label{},
				Label{Text: "Output ZIP"}, LineEdit{AssignTo: &outputEdit}, PushButton{Text: "Browse…", OnClicked: browseOutput},
			}},
			VSpacer{}, Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				HSpacer{}, PushButton{AssignTo: &createButton, Text: "Create ZIP", OnClicked: func() {
					var ids []string
					for _, id := range strings.Split(gamesEdit.Text(), ",") {
						if value := strings.TrimSpace(strings.ToLower(id)); value != "" {
							ids = append(ids, value)
						}
					}
					err := createModPackage(sourceEdit.Text(), idEdit.Text(), nameEdit.Text(), versionEdit.Text(), ids, minimumEdit.Text(), outputEdit.Text())
					if err != nil {
						walk.MsgBox(dlg, "Package creation failed", err.Error(), walk.MsgBoxIconError)
						return
					}
					walk.MsgBox(dlg, "Mod package created", "Created and hashed:\r\n"+outputEdit.Text(), walk.MsgBoxIconInformation)
					dlg.Accept()
				}}, PushButton{AssignTo: &cancelButton, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
			}},
		},
	}
	_, err := decl.Run(owner)
	_ = createButton
	_ = cancelButton
	if err != nil {
		walk.MsgBox(owner, "Package builder", err.Error(), walk.MsgBoxIconError)
	}
}

func executableDirectory() string {
	path, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(path)
}
