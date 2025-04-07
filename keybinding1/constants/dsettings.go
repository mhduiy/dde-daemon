// SPDX-FileCopyrightText: 2018 - 2022 UnionTech Software Technology Co., Ltd.
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package constants provides shared constants for the keybinding daemon
package constants

// DSettings related constants
const (
	DSettingsAppID = "org.deepin.dde.daemon"

	DSettingsKeyBindingName                = "org.deepin.dde.daemon.keybinding"
	DSettingsKeyWirelessControlEnable      = "wirelessControlEnable"
	DSettingsKeyNeedXrandrQDevices         = "need-xrandr-q-devices"
	DSettingsKeyDeviceManagerControlEnable = "deviceManagerControlEnable"

	DSettingsKeybindingPlatformId    = "org.deepin.dde.daemon.keybinding.platform"
	DSettingsKeybindingMediaKeyId    = "org.deepin.dde.daemon.keybinding.mediakey"
	DSettingsKeybindingSystemKeysId  = "org.deepin.dde.daemon.keybinding.system"
	DSettingsKeybindingWrapGnomeWmId = "org.deepin.dde.daemon.keybinding.wrap.gnome.wm"
	DSettingsKeybindingEnableId      = "org.deepin.dde.daemon.keybinding.enable"
	DSettingsKeyboardId              = "org.deepin.dde.daemon.keyboard"

	DSettingsKeyUpperLayerWLAN               = "upper-layer-wlan"
	DSettingsPowerId                         = "org.deepin.dde.daemon.power"
	DSettingsKeyBatteryPressPowerBtnAction   = "batteryPressPowerButton"
	DSettingsKeyLinePowerPressPowerBtnAction = "linePowerPressPowerButton"
	DSettingsKeyScreenBlackLock              = "screenBlackLock"
	DSettingsKeyHighPerformanceEnabled       = "highPerformanceEnabled"
	DSettingsKeySleepLock                    = "sleepLock"

	DSettingsKeyNumLockState             = "numlock-state"
	DSettingsKeySaveNumLockState         = "save-numlock-state"
	DSettingsKeyShortcutSwitchLayout     = "shortcut-switch-layout"
	DSettingsKeyShowCapsLockOSD          = "capslock-toggle"
	DSettingsKeyOsdAdjustBrightnessState = "osd-adjust-brightness-enabled"
	DSettingsKeyOsdAdjustVolumeState     = "osd-adjust-volume-enabled"

	DSettingsKeyAmbientLightAdjustBrightness = "ambientLightAdjustBrightness"
)
