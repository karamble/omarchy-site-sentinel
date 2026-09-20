import QtQuick
import QtQuick.Layouts
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

// The bar answers "is anything wrong with a site right now". The panel opens on
// the dashboard. All the checking happens in bin/sentinel daemon; this reads one
// JSON document from bin/sentinel and renders it.
Panel {
  id: root
  moduleName: "karamble.sitesentinel"
  ipcTarget: "karamble.sitesentinel"

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  // ---- settings, every fallback mirrors a manifest default
  readonly property string addr: setting("addr", "127.0.0.1:8098")
  readonly property int refreshSec: Math.max(5, Number(setting("refreshSec", 60)))
  readonly property bool certWarnsBar: setting("certWarnsBar", true) === true

  // ---- state fed by the helper
  property var snap: null
  property string view: "dashboard"

  // The keyboard cursor into the active view's rows; -1 means nothing is under
  // it. A property rather than focus, because a row is a drawing.
  property int cursor: -1
  property int actionIndex: 0
  onCursorChanged: {
    root.actionIndex = 0
    Qt.callLater(root.revealCursor)
  }

  // Every view answers rowCount, actionCount(row), activateRow(row, action) and
  // formFocused. The panel never learns what a row is.
  readonly property var activeView: viewLoader.item

  function clampCursor() {
    if (!root.activeView) { root.cursor = -1; return }
    if (root.cursor >= root.activeView.rowCount)
      root.cursor = root.activeView.rowCount - 1
  }
  onSnapChanged: root.clampCursor()

  // cursorItem walks down for whoever is painting the cursor, so it can be
  // scrolled into view.
  function cursorItem(item) {
    if (!item || !item.visible) return null
    if (item.hasCursor === true) return item
    var kids = item.children || []
    for (var i = 0; i < kids.length; i++) {
      var hit = root.cursorItem(kids[i])
      if (hit) return hit
    }
    return null
  }

  function revealCursor() {
    if (root.cursor < 0) return
    flick.revealItem(root.cursorItem(contentCol))
  }

  // Up and down walk rows, left and right walk the actions on the current row.
  function moveCursor(dx, dy) {
    if (!root.activeView) return
    if (dy !== 0) {
      var n = root.activeView.rowCount
      if (n <= 0) return
      root.cursor = root.cursor < 0 ? (dy > 0 ? 0 : n - 1)
                                    : (root.cursor + dy + n) % n
      return
    }
    if (dx !== 0) {
      var actions = root.activeView.actionCount(root.cursor)
      if (actions <= 1) return
      root.actionIndex = (root.actionIndex + dx + actions) % actions
    }
  }

  function activateCursor() {
    if (!root.activeView || root.cursor < 0) return
    root.activeView.activateRow(root.cursor, root.actionIndex)
  }

  // Focus returns to the key catcher whenever a form lets go, so the letter
  // shortcuts work again.
  function reclaimKeys() {
    if (keyCatcher) keyCatcher.forceActiveFocus()
  }

  property bool helperMissing: false
  // True when a source file is newer than the built helper, which is what an
  // update leaves behind: bin/ survives and goes stale.
  property bool helperStale: false
  property string lastError: ""
  property string actionError: ""

  // A refresh asks the daemon to probe every site, which takes seconds, while
  // the request itself returns at once. Reading the status straight afterwards
  // therefore returned the data from before the round, and nothing appeared to
  // happen until the poll timer next landed. checking holds the button in a
  // waiting state while lastChecked is watched for the round arriving.
  property bool checking: false
  property string checkingFrom: ""
  property int checkingTries: 0

  // fetchProc cannot be started while it is already running, and dropping the
  // request meant a fetch asked for right after a change was simply lost -- a
  // deleted row then sat there until the next poll. Remember the ask instead.
  property bool fetchQueued: false

  // The site a remove was asked for, cleared as soon as the answer is in.
  property string pendingRemoval: ""

  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family

  readonly property var rows: snap && snap.rows ? snap.rows : []
  readonly property var health: snap && snap.health ? snap.health : null
  readonly property var alerts: snap && snap.alerts ? snap.alerts : []
  property var alertList: []
  property var catalogue: []

  readonly property bool monitoring: root.health ? root.health.monitoring === true : true
  readonly property bool mcpEnabled: root.health ? root.health.mcpEnabled === true : false
  readonly property bool localFault: root.health ? root.health.localFault === true : false
  readonly property int certWarnDays: root.health && root.health.certWarnDays > 0 ? root.health.certWarnDays : 14
  readonly property int certUrgentDays: root.health && root.health.certUrgentDays > 0 ? root.health.certUrgentDays : 7
  readonly property int domainWarnDays: root.health && root.health.domainWarnDays > 0 ? root.health.domainWarnDays : 30

  readonly property int downCount: root.health ? Number(root.health.down || 0) : 0
  readonly property int siteCount: root.health ? Number(root.health.sites || 0) : 0
  readonly property int armedCount: {
    var n = 0
    for (var i = 0; i < root.alertList.length; i++) {
      var st = String(root.alertList[i].status || "")
      if (st === "armed" || st === "rearming" || st === "no-sample") n++
    }
    return n
  }

  readonly property int certProblems: {
    var n = 0
    for (var i = 0; i < root.rows.length; i++) {
      var r = root.rows[i]
      if (!r.certChecked) continue
      if (!r.certValid || !r.certCoversHost || r.certDays <= root.certWarnDays) n++
    }
    return n
  }

  // The worst state anything is in, which colours the bar.
  readonly property string level: {
    if (!root.monitoring) return "asleep"
    if (root.localFault) return "offline"
    if (root.downCount > 0) return "urgent"
    if (root.certWarnsBar && root.certProblems > 0) return "warn"
    return "clear"
  }

  // Nerd Font glyphs, written as real characters because escapes are not
  // decoded here.
  readonly property string iconShield: ""
  readonly property string iconRefresh: ""
  readonly property string iconCheck: ""
  readonly property string iconCross: ""
  readonly property string iconLock: ""
  readonly property string iconGlobe: ""
  readonly property string iconWarn: ""
  readonly property string iconDot: ""
  readonly property string iconBell: ""
  readonly property string iconCog: ""
  readonly property string iconPlus: ""
  readonly property string iconTrash: ""
  readonly property string iconPause: ""

  // Green is not in the theme palette, and a healthy site has to read as
  // distinct from a merely informational row.
  readonly property color toneOk: "#22c55e"

  function toneFor(sev) {
    if (sev === "urgent") return Color.urgent
    if (sev === "warn") return Color.accent
    if (sev === "info") return Qt.darker(root.foreground, 1.2)
    return root.toneOk
  }

  // A closed environment for every child: a PATH for the two absolute tools
  // that need none, a HOME so the helper finds its configuration, and nothing
  // else. Proxy settings and trust roots stay out.
  readonly property var childEnv: ({
    "PATH": "/usr/bin:/bin",
    "HOME": Quickshell.env("HOME") || ""
  })

  readonly property string pluginDir: Qt.resolvedUrl(".").toString().replace(/^file:\/\//, "").replace(/\/$/, "")
  readonly property string helperPath: pluginDir + "/bin/sentinel"

  function refresh() {
    if (!helperProbe.running) helperProbe.running = true
    if (!root.helperMissing && !staleProbe.running) staleProbe.running = true
    if (fetchProc.running) {
      // Queued rather than discarded: whatever prompted this wants data from
      // after it happened, and the fetch in flight was started before.
      root.fetchQueued = true
    } else {
      fetchProc.running = true
    }
    if (root.view === "alerts" && !alertsProc.running) alertsProc.running = true
  }

  // The button says Refresh, so it has to mean the data and not just the view.
  function refreshNow() {
    if (controlProc.running) {
      root.refresh()
      return
    }
    // Remember where the round counter stood, so the reply can be recognised
    // when it arrives rather than guessed at by waiting a fixed time.
    root.checkingFrom = root.lastChecked()
    root.checkingTries = 0
    root.checking = true
    root.actionError = ""
    controlProc.args = ["refresh"]
    controlProc.running = true
  }

  // The daemon's own timestamp for the last completed round.
  function lastChecked() {
    return root.snap && root.snap.status && root.snap.status.lastChecked
      ? String(root.snap.status.lastChecked) : ""
  }

  function stopChecking() {
    root.checking = false
    root.checkingFrom = ""
    root.checkingTries = 0
  }

  function setView(name) {
    root.view = name
    root.cursor = -1
    flick.contentY = 0
    root.actionError = ""
    if (name === "alerts") root.loadAlertsData()
  }

  function loadAlertsData() {
    if (!alertsProc.running) alertsProc.running = true
    if (!catalogueProc.running) catalogueProc.running = true
  }

  function control(args) {
    if (controlProc.running) {
      // Silently doing nothing is the worst of the options: the click looked
      // like it worked. Say so instead, and let the caller try again.
      root.actionError = "still working on the last command, try again in a moment"
      return
    }
    root.actionError = ""
    controlProc.args = args
    controlProc.running = true
  }

  function addSite(url, name, expect) {
    var args = ["add", url]
    if (name) args = args.concat(["-name", name])
    if (expect) args = args.concat(["-expect", expect])
    root.control(args)
  }

  function setSiteEnabled(id, on) { root.control([on ? "resume" : "pause", id]) }

  // The id is remembered so the row can go as soon as the daemon confirms the
  // removal, rather than at whatever point the next status arrives. Not before
  // it confirms: a row that vanishes on a failed delete is how a site removed
  // from the store but left in the monitor's state went unnoticed.
  function removeSite(id) {
    root.pendingRemoval = id
    root.control(["remove", id, "-yes"])
  }

  // Drop one row from the snapshot in place. snap is reassigned rather than
  // edited, because rows is bound to it and only a new value re-fires the
  // binding -- which also runs clampCursor and takes the row out of the
  // dashboard's copy of the same list.
  function dropRow(id) {
    if (!id || !root.snap || !root.snap.rows) return
    var s = root.snap
    var kept = []
    for (var i = 0; i < s.rows.length; i++) {
      if (s.rows[i].id !== id) kept.push(s.rows[i])
    }
    if (kept.length === s.rows.length) return
    s.rows = kept
    root.snap = s
  }
  function disarm(id) { root.control(["disarm", id]) }

  function toggleMonitoring() {
    root.control(["monitoring", root.monitoring ? "off" : "on"])
  }

  function setMCP(on) {
    root.control(["mcp-endpoint", on ? "on" : "off"])
  }

  // Cadences and thresholds, as flag pairs. An omitted one is left alone.
  function setOption(flagName, value) {
    root.control(["set", flagName, String(value)])
  }

  // Recycling shows the new entry once, in a terminal, so it can be copied.
  function recycleToken() {
    if (root.helperMissing) { root.runBuild(); return }
    root.runHelper("recycle")
  }

  // The launcher embeds what it is given in a bash -c string, so anything
  // interpolated into a command has to be quoted for a shell. Single quotes
  // with '\'' escaping is the only form with no exceptions.
  function shellQuote(s) {
    return "'" + String(s).replace(/'/g, "'\\''") + "'"
  }

  // Absolute, so the launcher is not resolved through whatever PATH the shell
  // inherited. Detached, because a Process owned by this panel dies when the
  // terminal takes the focus that closes the card.
  readonly property string launcher:
    "/usr/share/omarchy/bin/omarchy-launch-floating-terminal-with-presentation"

  function runTerminal(command) {
    Quickshell.execDetached([root.launcher, command])
  }

  // make -C rather than "cd X && make": no command separator in the string at
  // all, and one quoted argument.
  function runBuild() {
    root.runTerminal("make -C " + root.shellQuote(root.pluginDir))
  }

  function runHelper(verb) {
    root.runTerminal(root.shellQuote(root.helperPath) + " " + verb)
  }

  function showMCP() {
    if (root.helperMissing) { root.runBuild(); return }
    root.runHelper("mcp")
  }


  // A helper that never exits would hold a StdioCollector filling without
  // bound, so every child gets a budget. Killing the leader is not enough: a
  // helper that spawned anything of its own would leave it behind, so the
  // whole group goes.
  readonly property int childBudgetMs: 20000

  Process { id: reaper; clearEnvironment: true; environment: root.childEnv }

  // Both the process and its group: setting running to false stops the child
  // itself, and the negative id reaches anything it spawned, when it leads a
  // group. A target that has already gone makes kill fail harmlessly.
  function reap(pid, name) {
    if (!pid || pid <= 0) return
    root.lastError = name + " exceeded its budget and was stopped"
    reaper.command = ["/usr/bin/kill", "-TERM", "--", String(pid), "-" + pid]
    reaper.running = true
  }

  Timer {
    id: watchdog
    interval: 1000
    repeat: true
    running: true
    property var budgets: ({})
    onTriggered: {
      var procs = [
        { p: helperProbe, n: "the helper probe" },
        { p: staleProbe, n: "the staleness probe" },
        { p: fetchProc, n: "the status read" },
        { p: controlProc, n: "the command" },
        { p: alertsProc, n: "the alerts read" },
        { p: catalogueProc, n: "the catalogue read" }
      ]
      for (var i = 0; i < procs.length; i++) {
        var e = procs[i]
        if (!e.p.running) { watchdog.budgets[e.n] = 0; continue }
        watchdog.budgets[e.n] = (watchdog.budgets[e.n] || 0) + watchdog.interval
        if (watchdog.budgets[e.n] >= root.childBudgetMs) {
          root.reap(e.p.processId, e.n)
          e.p.running = false
          watchdog.budgets[e.n] = 0
        }
      }
    }
  }

  Component.onCompleted: refresh()
  onOpenedChanged: if (opened) refresh()

  Timer {
    interval: root.refreshSec * 1000
    running: true
    repeat: true
    onTriggered: root.refresh()
  }

  // While a requested round is running, read the status back often enough to
  // notice it finish. The daemon answers the request immediately and probes
  // afterwards, so without this the panel showed pre-refresh data and the
  // button looked inert until the poll above happened to land late enough.
  // Capped so a round that never reports cannot leave the button waiting.
  Timer {
    interval: 2000
    running: root.checking
    repeat: true
    onTriggered: {
      root.checkingTries += 1
      if (root.checkingTries > 22) {
        root.stopChecking()
        return
      }
      root.refresh()
    }
  }

  // Ask the filesystem whether the helper exists. A running daemon is not
  // evidence: a re-clone deletes bin/ while the daemon carries on.
  Process {
    id: helperProbe
    clearEnvironment: true
    environment: root.childEnv
    command: ["/usr/bin/test", "-x", root.helperPath]
    onExited: function (code, status) {
      root.helperMissing = code !== 0
      if (root.helperMissing) root.snap = null
    }
  }

  // Compares source against the built helper rather than asking git, so a
  // local edit reads the same as an update.
  // One find, no shell and no pipe: stale means it printed a path. Skipped
  // while the helper is missing, since there is nothing to be newer than.
  // Test files are left out: they never reach the binary. The module files
  // are counted: a dependency change does.
  Process {
    id: staleProbe
    clearEnvironment: true
    environment: root.childEnv
    running: false
    command: ["/usr/bin/find", root.pluginDir,
              "(", "-name", "*.go", "-not", "-name", "*_test.go",
              "-o", "-name", "go.mod", "-o", "-name", "go.sum", ")",
              "-newer", root.helperPath, "-print", "-quit"]
    stdout: StdioCollector {
      onStreamFinished: root.helperStale = this.text.trim().length > 0
    }
  }

  Process {
    id: fetchProc
    clearEnvironment: true
    environment: root.childEnv
    command: [root.helperPath, "status", "-json", "-addr", root.addr]

    onExited: function (code, status) {
      if (code !== 0 && !root.snap) root.helperMissing = true
      // Something asked for a fetch while this one was in flight, and it
      // wanted the state after whatever it had just done.
      if (root.fetchQueued) {
        root.fetchQueued = false
        fetchProc.running = true
      }
    }

    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        var raw = String(text || "")
        if (raw.trim() === "") return
        try {
          root.snap = JSON.parse(raw)
          root.helperMissing = false
          root.lastError = ""
          // The round the refresh button asked for has landed once the
          // daemon's own timestamp moves on from what it was.
          if (root.checking && root.lastChecked() !== root.checkingFrom) root.stopChecking()
        } catch (e) {
          root.lastError = "unreadable helper output"
        }
      }
    }

    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        var msg = String(text || "").trim()
        if (msg !== "") root.lastError = msg.replace(/^sentinel: /, "").split("\n")[0]
      }
    }
  }

  Process {
    id: controlProc
    clearEnvironment: true
    environment: root.childEnv
    property var args: []
    command: [root.helperPath].concat(controlProc.args).concat(["-addr", root.addr])

    onExited: function (code, status) {
      // A clean exit is the daemon's confirmation: the CLI turns a
      // removed:false answer into a non-zero exit and a message on stderr.
      if (root.pendingRemoval !== "") {
        if (code === 0) root.dropRow(root.pendingRemoval)
        root.pendingRemoval = ""
      }
      if (code !== 0) root.stopChecking()
      root.refresh()
    }

    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        var msg = String(text || "").trim()
        if (msg !== "") root.actionError = msg.replace(/^sentinel: /, "").split("\n")[0]
      }
    }
  }

  Process {
    id: alertsProc
    clearEnvironment: true
    environment: root.childEnv
    command: [root.helperPath, "alerts", "-addr", root.addr]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        try {
          var reply = JSON.parse(String(text || ""))
          root.alertList = reply.alerts || []
        } catch (e) { }
      }
    }
  }

  Process {
    id: catalogueProc
    clearEnvironment: true
    environment: root.childEnv
    command: [root.helperPath, "catalogue", "-addr", root.addr]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        try {
          var reply = JSON.parse(String(text || ""))
          root.catalogue = reply.catalogue || []
        } catch (e) { }
      }
    }
  }

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    // One glyph: BarIconButton renders through OpticalGlyph, and a count beside
    // it clips.
    text: root.iconShield
    active: root.level === "urgent" || root.level === "warn"
    foreground: {
      if (root.helperMissing) return Qt.darker(root.foreground, 1.6)
      if (root.level === "asleep") return Qt.darker(root.foreground, 2.0)
      if (root.level === "urgent") return Color.urgent
      if (root.level === "warn") return Color.accent
      if (root.level === "offline") return Qt.darker(root.foreground, 1.4)
      return root.foreground
    }
    tooltipText: {
      if (root.helperMissing) return "Site Sentinel: not built yet, run make in the plugin directory"
      if (!root.snap) return "Site Sentinel: connecting"
      if (!root.monitoring) return "Site Sentinel: asleep\nNothing is being checked."
      if (root.localFault) return "Site Sentinel: no connectivity\nNothing known about any site right now."
      if (root.siteCount === 0) return "Site Sentinel: no sites watched yet"
      var lines = []
      if (root.downCount > 0) lines.push(root.downCount + " of " + root.siteCount + " down")
      else lines.push(root.siteCount + " sites, all up")
      if (root.certProblems > 0) lines.push(root.certProblems + " certificates need attention")
      return "Site Sentinel\n" + lines.join("\n")
    }
    onPressed: function (b) {
      if (root.opened) root.close()
      else root.open()
    }
  }

  KeyboardPanel {
    id: panel
    anchorItem: button
    owner: root
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(560))
    contentHeight: panel.fittedContentHeight(
      contentCol.implicitHeight + (legend.visible ? legend.implicitHeight + Style.space(10) : 0),
      Style.space(820))

    // Keys arrive here before any focused control, which is what lets a bare
    // letter mean something. A form sets blocked and typing is just typing.
    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      blocked: !!root.activeView && root.activeView.formFocused === true

      onCloseRequested: {
        if (root.activeView && root.activeView.formOpen === true)
          root.activeView.cancelForm()
        else root.close()
      }
      onTabRequested: function (direction) { root.switchPanel(direction) }
      onMoveRequested: function (dx, dy) { root.moveCursor(dx, dy) }
      onActivateRequested: root.activateCursor()
      onDeleteRequested: {
        if (root.activeView && root.cursor >= 0 && root.activeView.deleteRow)
          root.activeView.deleteRow(root.cursor)
      }

      onTextKey: function (t) {
        switch (String(t).toLowerCase()) {
        case "d":
          root.setView("dashboard")
          break
        case "w":
          root.setView("sites")
          break
        case "a":
          root.setView(root.view === "alerts" ? "dashboard" : "alerts")
          break
        case "s":
          root.setView(root.view === "settings" ? "dashboard" : "settings")
          break
        case "r":
          root.refreshNow()
          if (root.view === "alerts") root.loadAlertsData()
          break
        case "m":
          root.toggleMonitoring()
          break
        case "n":
          if (root.activeView && root.activeView.beginAdd) root.activeView.beginAdd()
          break
        }
      }

      Flickable {
        id: flick
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.bottom: legend.visible ? legend.top : parent.bottom
        anchors.bottomMargin: legend.visible ? Style.space(8) : 0
        contentWidth: width
        contentHeight: contentCol.implicitHeight
        interactive: contentHeight > height
        boundsBehavior: Flickable.StopAtBounds
        clip: true

        // reveal scrolls just far enough for the band y..y+h to sit inside the
        // viewport, with a little air around it.
        function reveal(y, h) {
          var pad = Style.space(8)
          var maxY = Math.max(0, contentHeight - height)
          if (maxY === 0) { contentY = 0; return }
          if (y < contentY + pad) contentY = Math.max(0, y - pad)
          else if (y + h > contentY + height - pad) contentY = Math.min(maxY, y + h - height + pad)
        }

        function revealItem(item) {
          if (!item) return
          var p = item
          while (p && p !== contentCol) p = p.parent
          if (!p) return
          var r = item.mapToItem(contentCol, 0, 0)
          reveal(r.y, item.height)
        }

        // A field reached by Tab is kept in view the same way a cursor is.
        readonly property Item focused: Window.activeFocusItem
        onFocusedChanged: Qt.callLater(function () { flick.revealItem(flick.focused) })

        // Content that shrinks under the viewport would otherwise leave a gap.
        onContentHeightChanged: {
          var maxY = Math.max(0, contentHeight - height)
          if (contentY > maxY) contentY = maxY
        }

        Column {
          id: contentCol
          anchors.left: parent.left
          anchors.right: parent.right
          anchors.top: parent.top
          spacing: Style.space(12)

          // ---------- header ----------
          Item {
            width: parent.width
            implicitHeight: Math.max(titleCol.implicitHeight, headerActions.implicitHeight)

            Column {
              id: titleCol
              anchors.left: parent.left
              anchors.right: headerActions.left
              anchors.rightMargin: Style.space(10)
              anchors.verticalCenter: parent.verticalCenter
              spacing: Style.space(2)

              Text {
                textFormat: Text.PlainText
                text: "SITE SENTINEL"
                color: root.foreground
                font.family: root.fontFamily
                font.pixelSize: Style.font.title
                font.bold: true
              }

              Text {
                textFormat: Text.PlainText
                text: {
                  if (root.helperMissing) return "NOT BUILT"
                  if (!root.snap) return "CONNECTING"
                  if (!root.monitoring) return "ASLEEP · NOTHING IS BEING CHECKED"
                  if (root.localFault) return "NO CONNECTIVITY · NOTHING KNOWN RIGHT NOW"
                  if (root.siteCount === 0) return "NO SITES WATCHED YET"
                  if (root.downCount > 0) return root.downCount + " OF " + root.siteCount + " DOWN"
                  if (root.certProblems > 0) return root.certProblems + " CERTIFICATES NEED ATTENTION"
                  return "ALL " + root.siteCount + " UP"
                }
                color: root.level === "urgent" ? Color.urgent
                     : root.level === "warn" ? Color.accent
                     : Qt.darker(root.foreground, 1.4)
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
                font.bold: true
                font.letterSpacing: 1.2
              }
            }

            RowLayout {
              id: headerActions
              anchors.right: parent.right
              anchors.verticalCenter: parent.verticalCenter
              spacing: Style.space(6)

              Button {
                // Says what it is doing while it does it. The round takes
                // seconds and used to give no sign of running at all.
                text: root.checking ? "Checking" : "Refresh"
                tooltipText: root.checking
                  ? "Checking every site now, this takes a moment"
                  : "Check every site now  (r)"
                iconText: root.iconRefresh
                foreground: root.foreground
                accent: Color.accent
                fontFamily: root.fontFamily
                fontSize: Style.font.caption
                bordered: true
                focusable: true
                visible: !root.helperMissing
                onClicked: root.refreshNow()
              }
            }
          }

          // ---------- navigation ----------
          Item {
            width: parent.width
            visible: !root.helperMissing
            height: Math.max(tabs.height, navIcons.height)

            ButtonGroup {
              id: tabs
              anchors.left: parent.left
              anchors.verticalCenter: parent.verticalCenter
              options: [
                { value: "dashboard", label: "Dashboard", tooltip: "Dashboard  (d)" },
                { value: "sites", label: "Sites", tooltip: "Watched sites  (w)" }
              ]
              value: root.view === "alerts" || root.view === "settings" ? "" : root.view
              foreground: root.foreground
              accent: Color.accent
              fontFamily: root.fontFamily
              onChanged: function (v) { if (v) root.setView(v) }
            }

            Row {
              id: navIcons
              anchors.right: parent.right
              anchors.verticalCenter: parent.verticalCenter
              spacing: Style.space(14)

              Text {
                anchors.verticalCenter: parent.verticalCenter
                textFormat: Text.PlainText
                text: root.iconBell
                color: (!root.monitoring && root.armedCount > 0) ? Color.urgent : root.foreground
                opacity: root.view === "alerts" ? 1.0 : (bellHover.hovered ? 0.8 : 0.45)
                font.family: root.fontFamily
                font.pixelSize: Style.font.icon

                HoverHandler { id: bellHover; cursorShape: Qt.PointingHandCursor }
                TapHandler {
                  onTapped: root.setView(root.view === "alerts" ? "dashboard" : "alerts")
                }
                PanelToolTip {
                  visible: bellHover.hovered
                  text: {
                    if (root.view === "alerts") return "Back to the dashboard  (a)"
                    if (!root.monitoring && root.armedCount > 0)
                      return "Alerts: " + root.armedCount + " armed, nothing checking  (a)"
                    if (root.armedCount > 0) return "Alerts: " + root.armedCount + " armed  (a)"
                    return "Alerts  (a)"
                  }
                }

                // A mark rather than a number: the glyph has no room for one.
                Rectangle {
                  visible: root.armedCount > 0 && root.view !== "alerts"
                  anchors.right: parent.right
                  anchors.top: parent.top
                  anchors.rightMargin: -Style.space(2)
                  anchors.topMargin: Style.space(1)
                  width: Style.space(6)
                  height: width
                  radius: width / 2
                  color: !root.monitoring ? Color.urgent : Color.accent
                }
              }

              Text {
                anchors.verticalCenter: parent.verticalCenter
                textFormat: Text.PlainText
                text: root.iconCog
                color: root.foreground
                opacity: root.view === "settings" ? 1.0 : (cogHover.hovered ? 0.8 : 0.45)
                font.family: root.fontFamily
                font.pixelSize: Style.font.icon

                HoverHandler { id: cogHover; cursorShape: Qt.PointingHandCursor }
                TapHandler {
                  onTapped: root.setView(root.view === "settings" ? "dashboard" : "settings")
                }
                PanelToolTip {
                  visible: cogHover.hovered
                  text: root.view === "settings" ? "Back to the dashboard  (s)" : "Settings  (s)"
                }
              }
            }
          }

          // ---------- the unbuilt state ----------
          Column {
            width: parent.width
            visible: root.helperMissing
            spacing: Style.space(10)

            Text {
              width: parent.width
              wrapMode: Text.WordWrap
              textFormat: Text.PlainText
              text: "Site Sentinel ships source only, so the helper has to be compiled once on this machine. It needs the Go toolchain."
              color: Qt.darker(root.foreground, 1.3)
              font.family: root.fontFamily
              font.pixelSize: Style.font.bodySmall
            }

            Button {
              text: "Build now"
              tooltipText: "Runs make in the plugin directory, in a terminal"
              iconText: root.iconRefresh
              foreground: root.foreground
              accent: Color.accent
              fontFamily: root.fontFamily
              bordered: true
              focusable: true
              onClicked: root.runBuild()
            }
          }

          // ---------- the helper is older than the source ----------
          Item {
            width: parent.width
            visible: root.helperStale && !root.helperMissing
            implicitHeight: staleRow.implicitHeight

            Row {
              id: staleRow
              width: parent.width
              spacing: Style.space(10)

              Text {
                anchors.verticalCenter: parent.verticalCenter
                width: parent.width - rebuildButton.width - Style.space(10)
                wrapMode: Text.WordWrap
                textFormat: Text.PlainText
                text: "The helper is older than the source. Rebuild it, or the daemon keeps running the previous version."
                color: Color.accent
                font.family: root.fontFamily
                font.pixelSize: Style.font.caption
              }

              Button {
                id: rebuildButton
                anchors.verticalCenter: parent.verticalCenter
                text: "Rebuild"
                tooltipText: "Runs make in the plugin directory"
                iconText: root.iconRefresh
                foreground: root.foreground
                accent: Color.accent
                fontFamily: root.fontFamily
                fontSize: Style.font.caption
                bordered: true
                focusable: true
                onClicked: root.runBuild()
              }
            }
          }

          // ---------- errors ----------
          Text {
            width: parent.width
            wrapMode: Text.WordWrap
            textFormat: Text.PlainText
            visible: root.actionError !== "" || (root.lastError !== "" && !root.helperMissing)
            text: root.actionError !== "" ? root.actionError : root.lastError
            color: Color.urgent
            font.family: root.fontFamily
            font.pixelSize: Style.font.caption
          }

          // ---------- the active view ----------
          Loader {
            id: viewLoader
            width: parent.width
            visible: !root.helperMissing && !!root.snap
            active: visible
            source: {
              switch (root.view) {
              case "sites": return "SitesView.qml"
              case "alerts": return "AlertsView.qml"
              case "settings": return "SettingsView.qml"
              default: return "DashboardView.qml"
              }
            }
            onLoaded: {
              if (item) item.panel = root
              root.cursor = -1
            }
          }
        }
      }

      // The keys this view answers to, pinned so a long list cannot scroll it
      // away. Each entry is one key and what it does.
      Flow {
        id: legend
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.bottom: parent.bottom
        spacing: Style.space(10)
        visible: !root.helperMissing && !!root.snap

        readonly property var entries: {
          var out = [
            { k: "↑↓", v: "move" },
            { k: "⏎", v: root.view === "sites" ? "pause" : "open" },
            { k: "→", v: "actions" }
          ]
          if (root.view === "sites")
            out.push({ k: "n", v: "add" }, { k: "x", v: "remove" }, { k: "d", v: "dashboard" })
          else if (root.view === "alerts")
            out.push({ k: "n", v: "arm" }, { k: "x", v: "disarm" }, { k: "d", v: "dashboard" })
          else if (root.view === "settings")
            out.push({ k: "d", v: "dashboard" }, { k: "m", v: root.monitoring ? "pause checking" : "resume checking" })
          else
            out.push({ k: "w", v: "sites" }, { k: "a", v: "alerts" }, { k: "s", v: "settings" })
          out.push({ k: "r", v: "refresh" }, { k: "esc", v: "close" })
          return out
        }

        Repeater {
          model: legend.entries

          Row {
            required property var modelData
            spacing: Style.space(4)

            Text {
              textFormat: Text.PlainText
              text: modelData.k
              color: root.foreground
              font.family: root.fontFamily
              font.pixelSize: Style.font.caption
              font.bold: true
            }

            Text {
              textFormat: Text.PlainText
              text: modelData.v
              color: Qt.darker(root.foreground, 1.6)
              font.family: root.fontFamily
              font.pixelSize: Style.font.caption
            }
          }
        }
      }

      // A thin mark in the card padding rather than a stock scrollbar.
      Rectangle {
        visible: flick.contentHeight > flick.height
        anchors.right: parent.right
        anchors.rightMargin: -Style.space(6)
        width: Style.space(3)
        radius: width / 2
        color: Qt.rgba(root.foreground.r, root.foreground.g, root.foreground.b, 0.35)
        y: flick.visibleArea.yPosition * flick.height
        height: Math.max(Style.space(20), flick.visibleArea.heightRatio * flick.height)
      }
    }
  }
}
