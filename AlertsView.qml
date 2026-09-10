import QtQuick
import qs.Commons
import qs.Ui

// Armed triggers, with arm and disarm.
Column {
  id: view

  property var panel: null
  spacing: Style.space(10)

  readonly property var alerts: panel ? panel.alertList : []
  readonly property color fg: panel ? panel.foreground : Color.foreground
  readonly property string ff: panel ? panel.fontFamily : Style.font.family

  property bool adding: false
  property int confirming: -1

  onConfirmingChanged: if (confirming >= 0) confirmTimeout.restart()

  Timer {
    id: confirmTimeout
    interval: 4000
    onTriggered: view.confirming = -1
  }

  // ---- the view contract
  readonly property int rowCount: view.alerts.length
  readonly property bool formOpen: view.adding
  readonly property bool formFocused: view.adding && form.anyFieldFocused

  function actionCount(row) { return 2 }

  function activateRow(row, action) {
    if (row < 0 || row >= view.alerts.length) return
    if (action === 1) view.deleteRow(row)
  }

  function deleteRow(row) {
    if (row < 0 || row >= view.alerts.length) return
    if (view.confirming !== row) {
      view.confirming = row
      return
    }
    view.confirming = -1
    if (panel) panel.disarm(view.alerts[row].id)
  }

  function beginAdd() {
    view.adding = true
    Qt.callLater(function () { form.focusFirst() })
  }

  function cancelForm() {
    view.adding = false
    if (panel) panel.reclaimKeys()
  }

  Text {
    width: parent.width
    wrapMode: Text.WordWrap
    textFormat: Text.PlainText
    visible: panel && !panel.monitoring && view.alerts.length > 0
    text: "Checking is switched off, so nothing can trigger these."
    color: Color.urgent
    font.family: view.ff
    font.pixelSize: Style.font.caption
  }

  Item {
    width: parent.width
    height: addButton.height

    Button {
      id: addButton
      anchors.left: parent.left
      text: "Arm a watch"
      tooltipText: "Arm a watch  (n)"
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

  ArmForm {
    id: form
    width: parent.width
    visible: view.adding
    panel: view.panel
    fontFamily: view.ff
    foreground: view.fg

    onSubmitted: function (args) {
      if (panel) panel.control(args)
      view.cancelForm()
    }
    onCancelled: view.cancelForm()
  }

  Text {
    width: parent.width
    wrapMode: Text.WordWrap
    textFormat: Text.PlainText
    visible: view.alerts.length === 0 && !view.adding
    text: "Nothing is armed. A watch waits for a condition and then wakes you or an agent once."
    color: Qt.darker(view.fg, 1.3)
    font.family: view.ff
    font.pixelSize: Style.font.bodySmall
  }

  Repeater {
    model: view.alerts

    ListRow {
      required property int index
      required property var modelData

      width: view.width
      fontFamily: view.ff
      hasCursor: panel && panel.cursor === index
      actionIndex: panel ? panel.actionIndex : 0

      readonly property string status: String(modelData.status || "armed")

      tone: status === "spent" || status === "expired"
            ? Qt.darker(view.fg, 1.4)
            : (panel && !panel.monitoring ? Color.urgent : Color.accent)

      icon: panel ? panel.iconBell : ""
      title: String(modelData.path || "") + " " + String(modelData.operator || "")

      subtitle: {
        if (view.confirming === index) return "click again to disarm"
        var bits = []
        if (modelData.reason) bits.push(String(modelData.reason))
        if (modelData.armedBy) bits.push("armed by " + modelData.armedBy)
        if (modelData.deliverTo) bits.push("to " + modelData.deliverTo)
        return bits.join(" · ")
      }

      badges: {
        var out = [{ text: status, tone: status === "armed" ? Color.accent : Qt.darker(view.fg, 1.3), compact: true }]
        if (modelData.standing) out.push({ text: "standing", tone: Qt.darker(view.fg, 1.2), compact: true })
        return out
      }

      actionIcon: panel ? panel.iconTrash : ""
      actionTooltip: view.confirming === index ? "Click again to confirm" : "Disarm  (x)"
      actionTone: Color.urgent
      actionActive: view.confirming === index

      onActivated: if (panel) panel.cursor = index
      onActionTriggered: {
        if (panel) panel.cursor = index
        view.deleteRow(index)
      }
    }
  }
}
