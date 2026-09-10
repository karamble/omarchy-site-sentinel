import QtQuick
import qs.Commons

// A pill carrying one fact about a row. Colour carries the meaning.
Rectangle {
  id: badge

  property string text: ""
  property color tone: Color.accent
  property string fontFamily: Style.font.family
  // Loud badges are asking for something; quiet ones state a fact.
  property bool loud: false
  // Compact badges are small round labels, for wearing several at once.
  property bool compact: false

  implicitWidth: label.implicitWidth + Style.space(badge.compact ? 8 : 12)
  implicitHeight: Style.space(badge.compact ? 14 : 20)
  // Compact badges are fully rounded when the theme rounds anything.
  radius: Style.cornerRadius > 0
          ? (badge.compact ? height / 2 : Style.space(3))
          : 0
  visible: badge.text !== ""

  color: Qt.rgba(tone.r, tone.g, tone.b, badge.loud ? 0.20 : 0.10)
  border.color: Qt.rgba(tone.r, tone.g, tone.b, badge.loud ? 1.0 : 0.45)
  border.width: 1

  Text {
    id: label
    anchors.centerIn: parent
    textFormat: Text.PlainText
    text: badge.text
    color: badge.tone
    font.family: badge.fontFamily
    font.pixelSize: Math.round(Style.font.caption * (badge.compact ? 0.75 : 0.9))
    font.bold: !badge.compact
  }
}
