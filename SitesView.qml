import QtQuick
import qs.Commons
import qs.Ui

// The watched list, with add, pause and remove.
Column {
  id: view

  property var panel: null
  spacing: Style.space(10)

  readonly property var rows: panel ? panel.rows : []
  readonly property color fg: panel ? panel.foreground : Color.foreground
  readonly property string ff: panel ? panel.fontFamily : Style.font.family

  property bool adding: false
  // Which row is waiting for a second click to confirm removal. A dialog would
  // be better, but ConfirmDialog has no implicit size and collapses in a Column.
  property int confirming: -1

  onConfirmingChanged: if (confirming >= 0) confirmTimeout.restart()

  Timer {
    id: confirmTimeout
    interval: 4000
    onTriggered: view.confirming = -1
  }

  // ---- the view contract
  readonly property int rowCount: view.rows.length
  readonly property bool formOpen: view.adding
  readonly property bool formFocused: view.adding && form.anyFieldFocused

  // Two, because ListRow draws one trailing action: the row itself, then the
  // trash. A third would be a cursor stop nothing paints.
  function actionCount(row) { return 2 }

  function activateRow(row, action) {
    if (row < 0 || row >= view.rows.length) return
    var site = view.rows[row]
    if (action === 1) {
      view.deleteRow(row)
      return
    }
    if (panel) panel.setSiteEnabled(site.id, !site.enabled)
  }

  function deleteRow(row) {
    if (row < 0 || row >= view.rows.length) return
    if (view.confirming !== row) {
      view.confirming = row
      return
    }
    view.confirming = -1
    if (panel) panel.removeSite(view.rows[row].id)
  }

  function beginAdd() {
    view.adding = true
    Qt.callLater(function () { form.focusFirst() })
  }

  function cancelForm() {
    view.adding = false
    if (panel) panel.reclaimKeys()
  }

  Item {
    width: parent.width
    height: addButton.height

    Button {
      id: addButton
      anchors.left: parent.left
      text: "Watch a site"
      tooltipText: "Add a site  (n)"
      iconText: panel ? panel.iconPlus : ""
      foreground: view.fg
      accent: Color.accent
      fontFamily: view.ff
      fontSize: Style.font.caption
      bordered: true
      focusable: true
      visible: !view.adding
      onClicked: view.beginAdd()
    }
  }

  SiteForm {
    id: form
    width: parent.width
    visible: view.adding
    panel: view.panel
    fontFamily: view.ff
    foreground: view.fg

    onSubmitted: function (url, name, expect) {
      if (panel) panel.addSite(url, name, expect)
      view.cancelForm()
    }
    onCancelled: view.cancelForm()
  }

  Text {
    width: parent.width
    wrapMode: Text.WordWrap
    textFormat: Text.PlainText
    visible: view.rows.length === 0 && !view.adding
    text: "Nothing is being watched. Add a site and the sentinel starts checking it; until then it makes no network request at all."
    color: Qt.darker(view.fg, 1.3)
    font.family: view.ff
    font.pixelSize: Style.font.bodySmall
  }

  Repeater {
    model: view.rows

    ListRow {
      required property int index
      required property var modelData

      width: view.width
      fontFamily: view.ff
      hasCursor: panel && panel.cursor === index
      actionIndex: panel ? panel.actionIndex : 0

      tone: panel ? panel.toneFor(String(modelData.severity)) : Color.accent
      urgent: String(modelData.severity) === "urgent"
      icon: modelData.enabled ? (panel ? panel.iconGlobe : "") : (panel ? panel.iconPause : "")

      title: String(modelData.label || modelData.host || "")
      subtitle: {
        if (view.confirming === index) return "click again to stop watching this and forget its history"
        var bits = [String(modelData.url || "")]
        if (modelData.reason) bits.push(String(modelData.reason))
        return bits.join(" · ")
      }

      badges: {
        var out = []
        if (!modelData.enabled) out.push({ text: "paused", tone: Qt.darker(view.fg, 1.4), compact: true })
        if (modelData.domain) out.push({ text: String(modelData.domain), tone: Qt.darker(view.fg, 1.2), compact: true })
        return out
      }

      actionIcon: panel ? panel.iconTrash : ""
      actionTooltip: view.confirming === index ? "Click again to confirm" : "Stop watching  (x)"
      actionTone: Color.urgent
      actionActive: view.confirming === index

      // The mouse does what the keyboard does: the row toggles, the trash
      // removes.
      onActivated: {
        if (panel) panel.cursor = index
        view.activateRow(index, 0)
      }
      onActionTriggered: {
        if (panel) panel.cursor = index
        view.deleteRow(index)
      }
    }
  }
}
