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
  // Why the daemon last died, kept apart from lastError so that giving up
  // can report the cause rather than replace it. "gave up after 5 restarts"
  // describes this component, not the failure, and on its own it sends
  // somebody looking in the wrong place: the usual cause is another daemon
  // already holding the port, which the daemon itself names exactly.
  property string exitReason: ""

  function backoffMs() {
    return Math.min(30000, 1000 * Math.pow(2, root.restarts))
  }

  // The daemon logs to stderr as well as failing on it, so the first line is
  // usually a benign INFO record and the reason is further down. Structured
  // records are skipped rather than the last line taken blindly, so a warning
  // written after the failure cannot hide it.
  function failureLine(text) {
    var lines = String(text || "").split("\n")
    var reason = ""
    var lastAny = ""
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim()
      if (line === "") continue
      lastAny = line
      if (/(^|\s)level=(INFO|DEBUG)(\s|$)/.test(line)) continue
      reason = line
    }
    return reason !== "" ? reason : lastAny
  }

  // What to show when nothing is running: the cause when the daemon gave one,
  // and how many attempts it took either way.
  function gaveUpMessage() {
    var head = "gave up after " + root.maxRestarts + " restarts"
    return root.exitReason !== "" ? head + ": " + root.exitReason : head
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
        root.lastError = root.gaveUpMessage()
        return
      }
      root.restarts++
      respawn.interval = root.backoffMs()
      respawn.restart()
    }

    stderr: StdioCollector {
      waitForEnd: false
      onStreamFinished: {
        var reason = root.failureLine(text)
        if (reason === "") return
        root.exitReason = reason
        // The stream can finish either side of onExited, so a reason that
        // arrives late still reaches a message that has already been written.
        root.lastError = root.restarts >= root.maxRestarts ? root.gaveUpMessage() : reason
      }
    }
  }

  // Signal a process and its group. execDetached rather than a Process of our
  // own, because this is also called while this component is being destroyed,
  // and a Process owned by it would be torn down before it ran.
  //
  // Both the id and its negation: the first reaches the child, the second
  // reaches anything it spawned when it leads a group. A target that has
  // already gone makes kill fail harmlessly.
  function reap(pid) {
    if (!pid || pid <= 0) return
    Quickshell.execDetached(["/usr/bin/kill", "-TERM", "--", String(pid), "-" + pid])
  }

  // The probe is a one-shot test that should answer immediately. If it ever
  // does not, stop it rather than leaving a process and its collector alive.
  Timer {
    id: probeWatchdog
    interval: 10000
    repeat: false
    running: probe.running
    onTriggered: {
      if (!probe.running) return
      root.reap(probe.processId)
      probe.running = false
      root.lastError = "the build probe did not answer"
    }
  }

  Timer {
    id: respawn
    repeat: false
    onTriggered: if (!daemon.running) daemon.running = true
  }

  // Nothing destructive here: this also fires when the widget is toggled off.
  // Stopping the daemon is not destructive, and reaping its group is what stops
  // anything it spawned from outliving it.
  Component.onDestruction: {
    respawn.stop()
    probeWatchdog.stop()
    root.reap(daemon.processId)
    daemon.running = false
  }
}
