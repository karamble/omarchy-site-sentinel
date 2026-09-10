import QtQuick
import qs.Commons
import qs.Ui

// One row per site, worst first, with the two day counts as bubbles.
Column {
  id: view

  property var panel: null
  spacing: Style.space(10)

  readonly property var rows: panel ? panel.rows : []

  // ---- the view contract the panel drives
  readonly property int rowCount: view.rows.length
  readonly property bool formFocused: false
  readonly property bool formOpen: false

  function actionCount(row) { return 1 }

  function activateRow(row, action) {
    if (row < 0 || row >= view.rows.length) return
    if (panel) panel.setView("sites")
  }

  function cancelForm() { }

  Text {
    width: parent.width
    wrapMode: Text.WordWrap
    textFormat: Text.PlainText
    visible: view.rows.length === 0
    text: panel && panel.siteCount === 0
          ? "No sites are being watched yet. Nothing leaves this machine until you add one."
          : "Nothing has been checked yet."
    color: Qt.darker(panel ? panel.foreground : Color.foreground, 1.3)
    font.family: panel ? panel.fontFamily : Style.font.family
    font.pixelSize: Style.font.bodySmall
  }

  Repeater {
    model: view.rows

    ListRow {
      required property int index
      required property var modelData

      width: view.width
      fontFamily: panel ? panel.fontFamily : Style.font.family
      hasCursor: panel && panel.cursor === index
      actionIndex: panel ? panel.actionIndex : 0

      tone: panel ? panel.toneFor(String(modelData.severity)) : Color.accent
      urgent: String(modelData.severity) === "urgent"

      // The glyph follows severity, not just up or down: a site that answers
      // while its certificate is broken is not a green tick.
      icon: {
        if (!panel) return ""
        if (!modelData.probed) return panel.iconDot
        if (!modelData.up) return panel.iconCross
        var sev = String(modelData.severity)
        if (sev === "urgent" || sev === "warn") return panel.iconWarn
        return panel.iconCheck
      }

      title: String(modelData.label || modelData.host || "")

      // The host leads, so a row names the site it is about even when the
      // label is something else. It is dropped when the label already is it.
      subtitle: {
        var bits = []
        var host = String(modelData.host || "")
        if (host !== "" && host !== String(modelData.label || "")) bits.push(host)
        if (modelData.reason) bits.push(String(modelData.reason))
        else if (modelData.responseMs > 0) bits.push(modelData.responseMs + " ms")
        if (modelData.cdn) bits.push("behind " + modelData.cdn)
        else if (modelData.reverse) bits.push(String(modelData.reverse))
        else if (modelData.addrs && modelData.addrs.length > 0) bits.push(String(modelData.addrs[0]))
        return bits.join(" · ")
      }

      badges: {
        var out = []
        if (!modelData.enabled) {
          out.push({ text: "paused", tone: Qt.darker(view.fg, 1.4), compact: true })
          return out
        }
        if (modelData.certChecked) {
          if (!modelData.certCoversHost)
            out.push({ text: "wrong host", tone: Color.urgent, loud: true, compact: true })
          else if (!modelData.certValid)
            out.push({ text: "cert invalid", tone: Color.urgent, loud: true, compact: true })
          else
            out.push({
              text: "TLS " + modelData.certDays + "d",
              tone: view.certTone(modelData.certDays),
              compact: true
            })
        }
        if (modelData.domainKnown)
          out.push({
            text: "renews " + modelData.domainDays + "d",
            tone: view.domainTone(modelData.domainDays),
            compact: true
          })
        return out
      }

      onActivated: {
        if (panel) {
          panel.cursor = index
          panel.setView("sites")
        }
      }
    }
  }

  readonly property color fg: panel ? panel.foreground : Color.foreground

  function certTone(days) {
    if (!panel) return Color.accent
    if (days <= panel.certUrgentDays) return Color.urgent
    if (days <= panel.certWarnDays) return Color.accent
    return panel.toneOk
  }

  function domainTone(days) {
    if (!panel) return Color.accent
    if (days <= panel.domainWarnDays) return Color.accent
    return Qt.darker(panel.foreground, 1.2)
  }
}
