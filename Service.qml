import QtQuick
import Quickshell
import Quickshell.Io

// The daemon, owned by the shell. Built when the plugin is enabled, destroyed
// when it is disabled or removed. No systemd unit is installed.
Item {
  id: root

  visible: false
  width: 0
  height: 0

  // Injected by the shell when it constructs the service.
  property var shell: null
  property var manifest: null
  property var pluginRegistry: null

  readonly property string pluginDir: Qt.resolvedUrl(".").toString()
                                        .replace(/^file:\/\//, "").replace(/\/$/, "")
  readonly property string helperPath: pluginDir + "/bin/sentinel"

  // Respawn on failure, with backoff and a ceiling.
  readonly property int maxRestarts: 5
  property int restarts: 0
  property string lastError: ""

  function backoffMs() {
    return Math.min(30000, 1000 * Math.pow(2, root.restarts))
  }

  // Passed to every child. Built once so the two processes cannot drift.
  //
  // Deliberately short. PATH reaches notify-send and herdr, both in /usr/bin.
  // HOME finds the configuration. The session bus is what notify-send needs to
  // reach the notification daemon, and without it every alert would be
  // delivered into nothing. Proxy settings and trust roots are not here on
  // purpose: the daemon talks to sites the user named and nowhere else.
  readonly property var childEnv: ({
    "PATH": "/usr/bin:/bin",
    "HOME": Quickshell.env("HOME") || "",
    "XDG_RUNTIME_DIR": Quickshell.env("XDG_RUNTIME_DIR") || "",
    "DBUS_SESSION_BUS_ADDRESS": Quickshell.env("DBUS_SESSION_BUS_ADDRESS") || ""
  })

  // bin/ is not shipped, so a fresh clone has nothing to run.
  Process {
    id: probe
    command: ["/usr/bin/test", "-x", root.helperPath]
    running: true
    // A closed environment: the helper needs a PATH for nothing, a HOME to find
    // its configuration, and the runtime directory for desktop notifications.
    // Everything else, proxy settings and trust roots included, stays out.
    clearEnvironment: true
    environment: root.childEnv

    onExited: function (code, status) {
      if (code === 0) {
        root.lastError = ""
        if (!daemon.running) daemon.running = true
        return
      }
      root.lastError = "not built yet"
      rebprobe.restart()
    }
  }

  // Keep looking while there is nothing to run, so building the helper starts
  // the daemon without a shell restart.
  Timer {
    id: rebprobe
    interval: 5000
    repeat: false
    onTriggered: if (!daemon.running) probe.running = true
  }

  Process {
    id: daemon
    command: [root.helperPath, "daemon"]

    // A closed environment: the helper needs a PATH for nothing, a HOME to find
    // its configuration, and the runtime directory for desktop notifications.
    // Everything else, proxy settings and trust roots included, stays out.
    clearEnvironment: true
    environment: root.childEnv

    onExited: function (code, status) {
      // A clean exit means it was told to stop.
      if (code === 0) return
      if (root.restarts >= root.maxRestarts) {
        root.lastError = "gave up after " + root.maxRestarts + " restarts"
        return
      }
      root.restarts++
      respawn.interval = root.backoffMs()
      respawn.restart()
    }

    stderr: StdioCollector {
      waitForEnd: false
      onStreamFinished: {
        var msg = String(text || "").trim()
        if (msg !== "") root.lastError = msg.split("\n")[0]
      }
    }
  }

  Timer {
    id: respawn
    repeat: false
    onTriggered: if (!daemon.running) daemon.running = true
  }

  // Nothing destructive here: this also fires when the widget is toggled off.
  Component.onDestruction: {
    respawn.stop()
    daemon.running = false
  }
}
