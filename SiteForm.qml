import QtQuick
import qs.Commons
import qs.Ui

// The add-a-site form. Three fields and two buttons, in one explicit tab ring.
Column {
  id: form

  property var panel: null
  property string fontFamily: Style.font.family
  property color foreground: Color.foreground

  signal submitted(string url, string name, string expect)
  signal cancelled()

  spacing: Style.space(8)

  // The key catcher stands aside while any of these holds focus. Spelled out
  // rather than inferred: a field missed here silently swallows a shortcut.
  readonly property bool anyFieldFocused:
    urlField.activeFocus || nameField.activeFocus || expectField.activeFocus ||
    saveButton.activeFocus || cancelButton.activeFocus

  function focusFirst() { urlField.forceActiveFocus() }

  function submit() {
    var url = urlField.text.trim()
    if (url === "") return
    form.submitted(url, nameField.text.trim(), expectField.text.trim())
    urlField.text = ""
    nameField.text = ""
    expectField.text = ""
  }

  component Field: Column {
    property alias text: input.text
    property alias field: input
    property string label: ""
    property string hint: ""
    property string placeholder: ""

    width: form.width
    spacing: Style.space(2)

    Text {
      textFormat: Text.PlainText
      text: parent.label
      color: Qt.darker(form.foreground, 1.3)
      font.family: form.fontFamily
      font.pixelSize: Style.font.caption
      font.bold: true
    }

    TextField {
      id: input
      width: parent.width
      placeholderText: parent.placeholder
      foreground: form.foreground
      accent: Color.accent
      font.family: form.fontFamily
      Keys.onEscapePressed: form.cancelled()
      onAccepted: form.submit()
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      textFormat: Text.PlainText
      visible: parent.hint !== ""
      text: parent.hint
      color: Qt.darker(form.foreground, 1.5)
      font.family: form.fontFamily
      font.pixelSize: Style.font.caption
    }
  }

  Field {
    id: urlBlock
    label: "ADDRESS"
    placeholder: "https://example.com"
  }

  Field {
    id: nameBlock
    label: "NAME"
    placeholder: "optional; defaults to the hostname"
  }

  Field {
    id: expectBlock
    label: "EXPECT ON THE PAGE"
    placeholder: "optional"
    hint: "Without this a page returning 200 with an error on it counts as healthy."
  }

  Row {
    spacing: Style.space(8)

    Button {
      id: saveButton
      text: "Watch it"
      foreground: form.foreground
      accent: Color.accent
      fontFamily: form.fontFamily
      bordered: true
      focusable: true
      onClicked: form.submit()
    }

    Button {
      id: cancelButton
      text: "Cancel"
      foreground: form.foreground
      accent: Color.accent
      fontFamily: form.fontFamily
      bordered: true
      focusable: true
      onClicked: form.cancelled()
    }
  }

  // Explicit ring rather than activeFocusOnTab order: Qt skips links whose
  // target is invisible, which is what conditionally shown fields need.
  readonly property var urlField: urlBlock.field
  readonly property var nameField: nameBlock.field
  readonly property var expectField: expectBlock.field

  Component.onCompleted: {
    urlField.KeyNavigation.tab = nameField
    nameField.KeyNavigation.tab = expectField
    expectField.KeyNavigation.tab = saveButton
    saveButton.KeyNavigation.tab = cancelButton
    cancelButton.KeyNavigation.tab = urlField

    nameField.KeyNavigation.backtab = urlField
    expectField.KeyNavigation.backtab = nameField
    saveButton.KeyNavigation.backtab = expectField
    cancelButton.KeyNavigation.backtab = saveButton
    urlField.KeyNavigation.backtab = cancelButton
  }
}
