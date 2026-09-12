import QtQuick
import qs.Commons
import qs.Ui

// Cadences, thresholds, the kill switch and the MCP endpoint.
Column {
  id: view

  property var panel: null
  spacing: Style.space(14)

  readonly property color fg: panel ? panel.foreground : Color.foreground
  readonly property string ff: panel ? panel.fontFamily : Style.font.family

  // ---- the view contract. Settings are controls, not rows, so the cursor has
  // nothing to walk and Tab drives the whole view.
  readonly property int rowCount: 0
  readonly property bool formOpen: false
  readonly property bool formFocused:
    certWarn.activeFocus || certUrgent.activeFocus || domainWarn.activeFocus || reachEvery.activeFocus

  function actionCount(row) { return 0 }
  function activateRow(row, action) { }
  function cancelForm() { }

  component Section: Column {
    property string title: ""
    default property alias content: holder.data

    width: view.width
    spacing: Style.space(6)

    PanelSectionHeader {
      width: parent.width
      text: parent.title
      foreground: view.fg
      fontFamily: view.ff
    }

    Column {
      id: holder
      width: parent.width
      spacing: Style.space(8)
    }
  }

  component NumberRow: Row {
    property alias field: input
    property string label: ""
    property string suffix: ""
    property int value: 0
    property string option: ""

    width: view.width
    spacing: Style.space(8)

    Text {
      anchors.verticalCenter: parent.verticalCenter
      width: Style.space(200)
      textFormat: Text.PlainText
      text: parent.label
      color: view.fg
      font.family: view.ff
      font.pixelSize: Style.font.bodySmall
    }

    // A plain field with a validator, not a NumberField: NumberField wraps a
    // SpinBox and puts activeFocus on an inner item.
    TextField {
      id: input
      width: Style.space(80)
      text: String(parent.value)
      foreground: view.fg
      accent: Color.accent
      font.family: view.ff
      validator: IntValidator { bottom: 1 }
      onAccepted: {
        if (panel && text.trim() !== "") panel.setOption("-" + parent.option, text.trim())
      }
    }

    Text {
      anchors.verticalCenter: parent.verticalCenter
      textFormat: Text.PlainText
      text: parent.suffix
      color: Qt.darker(view.fg, 1.4)
      font.family: view.ff
      font.pixelSize: Style.font.caption
    }
  }

  Section {
    title: "CHECKING"

    Toggle {
      width: view.width
      label: panel && panel.monitoring ? "Checking is on" : "Checking is off"
      checked: panel ? panel.monitoring : true
      foreground: view.fg
      accent: Color.accent
      fontFamily: view.ff
      onClicked: if (panel) panel.toggleMonitoring()
    }

    Text {
      width: view.width
      wrapMode: Text.WordWrap
      textFormat: Text.PlainText
      text: "Off means nothing is checked and no request leaves this machine. Armed watches cannot trigger while it is off."
      color: Qt.darker(view.fg, 1.4)
      font.family: view.ff
      font.pixelSize: Style.font.caption
    }

    NumberRow {
      id: reachEveryRow
      label: "Check reachability every"
      suffix: "minutes"
      option: "reach"
      value: 5
    }
  }

  Section {
    title: "WARNING THRESHOLDS"

    NumberRow {
      id: certWarnRow
      label: "Warn about a certificate at"
      suffix: "days left"
      option: "cert-warn"
      value: panel ? panel.certWarnDays : 14
    }

    NumberRow {
      id: certUrgentRow
      label: "Treat it as urgent at"
      suffix: "days left"
      option: "cert-urgent"
      value: panel ? panel.certUrgentDays : 7
    }

    NumberRow {
      id: domainWarnRow
      label: "Mention a domain renewal at"
      suffix: "days left"
      option: "domain-warn"
      value: panel ? panel.domainWarnDays : 30
    }

    Text {
      width: view.width
      wrapMode: Text.WordWrap
      textFormat: Text.PlainText
      text: "Certificates that renew automatically usually do so with 30 days left, so warning above that fires on every ordinary renewal."
      color: Qt.darker(view.fg, 1.4)
      font.family: view.ff
      font.pixelSize: Style.font.caption
    }
  }

  Section {
    title: "AGENTS"

    Toggle {
      width: view.width
      label: panel && panel.mcpEnabled ? "MCP endpoint is answering" : "MCP endpoint is off"
      checked: panel ? panel.mcpEnabled : false
      foreground: view.fg
      accent: Color.accent
      fontFamily: view.ff
      onClicked: if (panel) panel.setMCP(!panel.mcpEnabled)
    }

    Row {
      spacing: Style.space(8)

      Button {
        text: "Show MCP entry"
        tooltipText: "Prints the entry to paste into an agent's config"
        foreground: view.fg
        accent: Color.accent
        fontFamily: view.ff
        fontSize: Style.font.caption
        bordered: true
        focusable: true
        onClicked: if (panel) panel.showMCP()
      }


      Button {
        text: "New token"
        tooltipText: "Mints a new API token, locking out every client holding the old one"
        foreground: view.fg
        accent: Color.urgent
        fontFamily: view.ff
        fontSize: Style.font.caption
        bordered: true
        focusable: true
        onClicked: if (panel) panel.recycleToken()
      }
    }
  }

  readonly property var certWarn: certWarnRow.field
  readonly property var certUrgent: certUrgentRow.field
  readonly property var domainWarn: domainWarnRow.field
  readonly property var reachEvery: reachEveryRow.field
}
