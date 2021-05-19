package gesture

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/godbus/dbus"
	daemon "github.com/linuxdeepin/go-dbus-factory/com.deepin.daemon.daemon"
	display "github.com/linuxdeepin/go-dbus-factory/com.deepin.daemon.display"
	gesture "github.com/linuxdeepin/go-dbus-factory/com.deepin.daemon.gesture"
	clipboard "github.com/linuxdeepin/go-dbus-factory/com.deepin.dde.clipboard"
	dock "github.com/linuxdeepin/go-dbus-factory/com.deepin.dde.daemon.dock"
	notification "github.com/linuxdeepin/go-dbus-factory/com.deepin.dde.notification"
	sessionmanager "github.com/linuxdeepin/go-dbus-factory/com.deepin.sessionmanager"
	wm "github.com/linuxdeepin/go-dbus-factory/com.deepin.wm"
	gio "pkg.deepin.io/gir/gio-2.0"
	"pkg.deepin.io/lib/dbusutil"
	"pkg.deepin.io/lib/dbusutil/proxy"
	"pkg.deepin.io/lib/gsettings"
	dutils "pkg.deepin.io/lib/utils"
)

//go:generate dbusutil-gen em -type Manager

const (
	tsSchemaID              = "com.deepin.dde.touchscreen"
	tsSchemaKeyLongPress    = "longpress-duration"
	tsSchemaKeyShortPress   = "shortpress-duration"
	tsSchemaKeyEdgeMoveStop = "edgemovestop-duration"
	tsSchemaKeyBlacklist    = "longpress-blacklist"
)

type Manager struct {
	wm             wm.Wm
	sysDaemon      daemon.Daemon
	systemSigLoop  *dbusutil.SignalLoop
	mu             sync.RWMutex
	userFile       string
	builtinSets    map[string]func() error
	gesture        gesture.Gesture
	dock           dock.Dock
	display        display.Display
	setting        *gio.Settings
	tsSetting      *gio.Settings
	enabled        bool
	Infos          gestureInfos
	sessionmanager sessionmanager.SessionManager
	clipboard      clipboard.Clipboard
	notification   notification.Notification
}

func newManager() (*Manager, error) {
	sessionConn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}

	systemConn, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}

	var filename = configUserPath
	if !dutils.IsFileExist(configUserPath) {
		filename = configSystemPath
	}

	infos, err := newGestureInfosFromFile(filename)
	if err != nil {
		return nil, err
	}
	// for touch long press
	infos = append(infos, &gestureInfo{
		Event: EventInfo{
			Name:      "touch right button",
			Direction: "down",
			Fingers:   0,
		},
		Action: ActionInfo{
			Type:   ActionTypeCommandline,
			Action: "xdotool mousedown 3",
		},
	})
	infos = append(infos, &gestureInfo{
		Event: EventInfo{
			Name:      "touch right button",
			Direction: "up",
			Fingers:   0,
		},
		Action: ActionInfo{
			Type:   ActionTypeCommandline,
			Action: "xdotool mouseup 3",
		},
	})

	setting, err := dutils.CheckAndNewGSettings(gestureSchemaId)
	if err != nil {
		return nil, err
	}

	tsSetting, err := dutils.CheckAndNewGSettings(tsSchemaID)
	if err != nil {
		return nil, err
	}

	m := &Manager{
		userFile:       configUserPath,
		Infos:          infos,
		setting:        setting,
		tsSetting:      tsSetting,
		enabled:        setting.GetBoolean(gsKeyEnabled),
		wm:             wm.NewWm(sessionConn),
		dock:           dock.NewDock(sessionConn),
		display:        display.NewDisplay(sessionConn),
		sysDaemon:      daemon.NewDaemon(systemConn),
		sessionmanager: sessionmanager.NewSessionManager(sessionConn),
		clipboard:      clipboard.NewClipboard(sessionConn),
		notification:   notification.NewNotification(sessionConn),
	}

	m.gesture = gesture.NewGesture(systemConn)
	m.systemSigLoop = dbusutil.NewSignalLoop(systemConn, 10)
	return m, nil
}

func (m *Manager) destroy() {
	m.gesture.RemoveHandler(proxy.RemoveAllHandlers)
	m.systemSigLoop.Stop()
	m.setting.Unref()
}

func (m *Manager) init() {
	m.initBuiltinSets()
	err := m.sysDaemon.SetLongPressDuration(0, uint32(m.tsSetting.GetInt(tsSchemaKeyLongPress)))
	if err != nil {
		logger.Warning("call SetLongPressDuration failed:", err)
	}
	err = m.gesture.SetShortPressDuration(0, uint32(m.tsSetting.GetInt(tsSchemaKeyShortPress)))
	if err != nil {
		logger.Warning("call SetShortPressDuration failed:", err)
	}
	err = m.gesture.SetEdgeMoveStopDuration(0, uint32(m.tsSetting.GetInt(tsSchemaKeyEdgeMoveStop)))
	if err != nil {
		logger.Warning("call SetEdgeMoveStopDuration failed:", err)
	}

	m.systemSigLoop.Start()
	m.gesture.InitSignalExt(m.systemSigLoop, true)
	_, err = m.gesture.ConnectEvent(func(name string, direction string, fingers int32) {
		should, err := m.shouldHandleEvent()
		if err != nil {
			logger.Error("shouldHandleEvent failed:", err)
			return
		}
		if !should {
			return
		}

		err = m.Exec(EventInfo{
			Name:      name,
			Direction: direction,
			Fingers:   fingers,
		})
		if err != nil {
			logger.Error("Exec failed:", err)
		}
	})
	if err != nil {
		logger.Error("connect gesture event failed:", err)
	}

	_, err = m.gesture.ConnectTouchEdgeMoveStopLeave(func(direction string, scaleX float64, scaleY float64, duration int32) {
		should, err := m.shouldHandleEvent()
		if err != nil {
			logger.Error("shouldHandleEvent failed:", err)
			return
		}
		if !should {
			return
		}

		err = m.handleTouchEdgeMoveStopLeave(direction, scaleX, scaleY, duration)
		if err != nil {
			logger.Error("handleTouchEdgeMoveStopLeave failed:", err)
		}
	})
	if err != nil {
		logger.Error("connect TouchEdgeMoveStopLeave failed:", err)
	}

	_, err = m.gesture.ConnectTouchEdgeEvent(func(direction string, scaleX float64, scaleY float64) {
		should, err := m.shouldHandleEvent()
		if err != nil {
			logger.Error("shouldHandleEvent failed:", err)
			return
		}
		if !should {
			return
		}

		err = m.handleTouchEdgeEvent(direction, scaleX, scaleY)
		if err != nil {
			logger.Error("handleTouchEdgeEvent failed:", err)
		}
	})
	if err != nil {
		logger.Error("connect handleTouchEdgeEvent failed:", err)
	}

	_, err = m.gesture.ConnectTouchMovementEvent(func(direction string, fingers int32, startScaleX float64, startScaleY float64, endScaleX float64, endScaleY float64) {
		should, err := m.shouldHandleEvent()
		if err != nil {
			logger.Error("shouldHandleEvent failed:", err)
			return
		}
		if !should {
			return
		}

		err = m.handleTouchMovementEvent(direction, fingers, startScaleX, startScaleY, endScaleX, endScaleY)
		if err != nil {
			logger.Error("handleTouchMovementEvent failed:", err)
		}
	})
	if err != nil {
		logger.Error("connect handleTouchMovementEvent failed:", err)
	}
	m.listenGSettingsChanged()
}

func (m *Manager) shouldIgnoreGesture(info *gestureInfo) bool {
	// allow right button up when kbd grabbed
	if (info.Event.Name != "touch right button" || info.Event.Direction != "up") && isKbdAlreadyGrabbed() {
		logger.Debug("another process grabbed keyboard, not exec action")
		return true
	}

	// TODO(jouyouyun): improve touch right button handler
	if info.Event.Name == "touch right button" {
		// filter google chrome
		if isInWindowBlacklist(getCurrentActionWindowCmd(), m.tsSetting.GetStrv(tsSchemaKeyBlacklist)) {
			logger.Debug("the current active window in blacklist")
			return true
		}
	} else if strings.HasPrefix(info.Event.Name, "touch") {
		return true
	}

	return false
}

func (m *Manager) Exec(evInfo EventInfo) error {
	info := m.Infos.Get(evInfo)
	if info == nil {
		return fmt.Errorf("not found event info: %s", evInfo.toString())
	}

	logger.Debugf("[Exec]: event info:%s  action info:%s", info.Event.toString(), info.Action.toString())
	if m.shouldIgnoreGesture(info) {
		return nil
	}

	var cmd = info.Action.Action
	switch info.Action.Type {
	case ActionTypeCommandline:
		break
	case ActionTypeShortcut:
		cmd = fmt.Sprintf("xdotool key %s", cmd)
	case ActionTypeBuiltin:
		return m.handleBuiltinAction(cmd)
	default:
		return fmt.Errorf("invalid action type: %s", info.Action.Type)
	}

	out, err := exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", string(out))
	}
	return nil
}

func (m *Manager) Write() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	err := os.MkdirAll(filepath.Dir(m.userFile), 0755)
	if err != nil {
		return err
	}
	data, err := json.Marshal(m.Infos)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(m.userFile, data, 0644)
}

func (m *Manager) listenGSettingsChanged() {
	gsettings.ConnectChanged(gestureSchemaId, gsKeyEnabled, func(key string) {
		m.mu.Lock()
		m.enabled = m.setting.GetBoolean(key)
		m.mu.Unlock()
	})
}

func (m *Manager) handleBuiltinAction(cmd string) error {
	fn := m.builtinSets[cmd]
	if fn == nil {
		return fmt.Errorf("invalid built-in action %q", cmd)
	}
	return fn()
}

func (*Manager) GetInterfaceName() string {
	return dbusServiceIFC
}

//param @edge: swipe to touchscreen edge
func (m *Manager) handleTouchEdgeMoveStopLeave(edge string, scaleX float64, scaleY float64, duration int32) error {
	if edge == "bot" {
		position, err := m.dock.Position().Get(0)
		if err != nil {
			logger.Error("get dock.Position failed:", err)
			return err
		}

		if position >= 0 {
			rect, err := m.dock.FrontendWindowRect().Get(0)
			if err != nil {
				logger.Error("get dock.FrontendWindowRect failed:", err)
				return err
			}

			var dockPly uint32 = 0
			if position == positionTop || position == positionBottom {
				dockPly = rect.Height
			} else if position == positionRight || position == positionLeft {
				dockPly = rect.Width
			}

			screenHeight, err := m.display.ScreenHeight().Get(0)
			if err != nil {
				logger.Error("get display.ScreenHeight failed:", err)
				return err
			}

			if screenHeight > 0 && float64(dockPly)/float64(screenHeight)+scaleY < 1 {
				return m.handleBuiltinAction("ShowWorkspace")
			}
		}
	}
	return nil
}

//处理触摸屏滑动手势

func (m *Manager) handleTouchEdgeEvent(edge string, scaleX float64, scaleY float64) error {
	sessionBus, err := dbus.SessionBus()
	if err != nil {
		logger.Error(err)
	}
	obj := sessionBus.Object("com.deepin.daemon.Display", "/com/deepin/daemon/Display")
	var ret dbus.Variant
	var ret1 dbus.Variant
	err = obj.Call("org.freedesktop.DBus.Properties.Get", 0, "com.deepin.daemon.Display", "Monitors").Store(&ret)
	if err != nil {
		logger.Error(err)
	}
	err = obj.Call("org.freedesktop.DBus.Properties.Get", 0, "com.deepin.daemon.Display", "TouchMap").Store(&ret1)
	if err != nil {
		logger.Error(err)
	}
	dbusObjectPathArray := ret.Value().([]dbus.ObjectPath)
	mapTouchName := ret1.Value().(map[string]string)
	var screenName string
	if len(mapTouchName) > 0 {
		for _, screenName = range mapTouchName {
			break
		}
	} else {
		logger.Warning("The number of touch screen cannot be 0 or less. ")
		return nil
	}
	var rotation uint16

	for _, dbusObjectPath := range dbusObjectPathArray {
		obj := sessionBus.Object("com.deepin.daemon.Display", dbusObjectPath)
		var name string
		err = obj.Call("org.freedesktop.DBus.Properties.Get", 0, "com.deepin.daemon.Display.Monitor", "Name").Store(&name)
		if err != nil {
			logger.Error(err)
		}
		if name == screenName {
			err = obj.Call("org.freedesktop.DBus.Properties.Get", 0, "com.deepin.daemon.Display.Monitor", "Rotation").Store(&rotation)
			if err != nil {
				logger.Error(err)
			}
			break
		}
	}
	err = m.handleRotationTouchEdgeEvent(rotation, edge, scaleX, scaleY)
	return err
}

//处理屏幕旋转后的滑动手势
func (m *Manager) handleRotationTouchEdgeEvent(rotation uint16, edge string, scaleX float64, scaleY float64) error {
	var cmd = ""
	screenHeight, err := m.display.ScreenHeight().Get(0)
	if err != nil {
		logger.Error("get display.ScreenHeight failed:", err)
		return err
	}
	screenWight, err := m.display.ScreenWidth().Get(0)
	if err != nil {
		logger.Error("get display.ScreenWidth failed:", err)
		return err
	}
	//不旋转
	if rotation == 1 {
		if edge == "left" {
			if scaleX*float64(screenWight) > 100 {
				cmd = "xdotool key ctrl+alt+v"
			}
		}
		if edge == "right" {
			if (1-scaleX)*float64(screenWight) > 100 {
				cmd = "dbus-send --type=method_call --dest=com.deepin.dde.osd /org/freedesktop/Notifications com.deepin.dde.Notification.Toggle"
			}
		}
	}
	//旋转90度
	if rotation == 2 {
		if edge == "bot" {
			if scaleY*float64(screenHeight) > 100 {
				cmd = "xdotool key ctrl+alt+v"
			}
		}
		if edge == "top" {
			if (1-scaleY)*float64(screenHeight) > 100 {
				cmd = "dbus-send --type=method_call --dest=com.deepin.dde.osd /org/freedesktop/Notifications com.deepin.dde.Notification.Toggle"
			}
		}
	}
	//旋转180度
	if rotation == 4 {
		if edge == "left" {
			if scaleX*float64(screenWight) > 100 {
				cmd = "dbus-send --type=method_call --dest=com.deepin.dde.osd /org/freedesktop/Notifications com.deepin.dde.Notification.Toggle"

			}
		}
		if edge == "right" {
			if (1-scaleX)*float64(screenWight) > 100 {
				cmd = "xdotool key ctrl+alt+v"
			}
		}
	}
	//旋转270度
	if rotation == 8 {
		if edge == "bot" {
			if scaleY*float64(screenHeight) > 100 {
				cmd = "dbus-send --type=method_call --dest=com.deepin.dde.osd /org/freedesktop/Notifications com.deepin.dde.Notification.Toggle"
			}
		}
		if edge == "top" {
			if (1-scaleY)*float64(screenHeight) > 100 {
				cmd = "xdotool key ctrl+alt+v"
			}
		}
	}

	if len(cmd) != 0 {
		out, err := exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s", string(out))
		}
	}

	return nil
}

func (m *Manager) handleTouchMovementEvent(direction string, fingers int32, startScaleX float64, startScaleY float64, endScaleX float64, endScaleY float64) error {
	if fingers == 1 {
		switch direction {
		case "left":
			return m.clipboard.Hide(0)
		case "right":
			return m.notification.Hide(0)
		}
	}

	return nil
}

//touchpad double click down
func (m *Manager) handleDbclickDown(fingers int32) error {
	if fingers == 3 {
		return m.wm.TouchToMove(0, 0, 0)
	}
	return nil
}

//touchpad swipe move
func (m *Manager) handleSwipeMoving(fingers int32, accelX float64, accelY float64) error {
	if fingers == 3 {
		return m.wm.TouchToMove(0, int32(accelX), int32(accelY))
	}
	return nil
}

//touchpad swipe stop or interrupted
func (m *Manager) handleSwipeStop(fingers int32) error {
	if fingers == 3 {
		return m.wm.ClearMoveStatus(0)
	}
	return nil
}

// 多用户存在，防止非当前用户响应触摸屏手势
func (m *Manager) shouldHandleEvent() (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.enabled {
		return false, nil
	}

	currentSessionPath, err := m.sessionmanager.CurrentSessionPath().Get(0)
	if err != nil {
		return false, fmt.Errorf("get login1 session path failed: %v", err)
	}

	if !isSessionActive(currentSessionPath) {
		return false, nil
	}

	return true, nil
}
