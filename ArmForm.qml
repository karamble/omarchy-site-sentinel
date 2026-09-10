import QtQuick
import qs.Commons
import qs.Ui

// The arm-a-watch form. Which fields are shown depends on the operator the
// chosen path accepts.
Column {
  id: form

  property var panel: null
  property string fontFamily: Style.font.family
  property color foreground: Color.foreground

  // Emits the argument list for the helper, so the panel and the command line
  // put the same thing on the wire.
  signal submitted(var args)
  signal cancelled()

  spacing: Style.space(8)

  readonly property var catalogue: panel ? panel.catalogue : []
  property string path: ""
  property string operator: ""

  readonly property var leaf: {
    for (var i = 0; i < form.catalogue.length; i++)
      if (String(form.catalogue[i].path) === form.path) return form.catalogue[i]
    return null
  }

  readonly property var operators: form.leaf && form.leaf.operators ? form.leaf.operators : []
  readonly property bool wantsBound: form.operator === "crosses" || form.operator === "count"
  readonly property bool wantsValue: form.operator === "becomes"
  readonly property bool wantsAge: form.operator === "ages"

  readonly property bool anyFieldFocused:
    boundField.activeFocus || valueField.activeFocus || ageField.activeFocus ||
    expiresField.activeFocus || reasonField.activeFocus ||
    saveButton.activeFocus || cancelButton.activeFocus ||
    pathMenu.popupOpen || opMenu.popupOpen

  function focusFirst() { pathMenu.forceActiveFocus() }

  function submit() {
    if (form.path === "" || form.operator === "") return
    var expires = expiresField.text.trim()
    if (expires === "") expires = "30d"

    var args = ["arm", "-path", form.path, "-op", form.operator, "-expires", expires]
    if (form.wantsBound && boundField.text.trim() !== "")
      args = args.concat([aboveBelow.value === "above" ? "-above" : "-below", boundField.text.trim()])
    if (form.wantsValue && valueField.text.trim() !== "")
      args = args.concat(["-value", valueField.text.trim()])
    if (form.wantsAge && ageField.text.trim() !== "")
      args = args.concat(["-older-than", ageField.text.trim()])
    if (reasonField.text.trim() !== "")
      args = args.concat(["-reason", reasonField.text.trim()])
    if (standing.checked) args.push("-standing")

    form.submitted(args)
    boundField.text = ""
    valueField.text = ""
    ageField.text = ""
    reasonField.text = ""
  }

  component Labelled: Column {
    property string label: ""
    property string hint: ""
    default property alias content: holder.data

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

    Item {
      id: holder
      width: parent.width
      implicitHeight: childrenRect.height
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

  Labelled {
    label: "WATCH"

    SearchableDropdown {
      id: pathMenu
      width: form.width
      foreground: form.foreground
      accent: Color.accent
      fontFamily: form.fontFamily
      options: {
        var out = []
        for (var i = 0; i < form.catalogue.length; i++) {
          var l = form.catalogue[i]
          out.push({ value: String(l.path), label: String(l.path), tooltip: String(l.describes || "") })
        }
        return out
      }
      value: form.path
      onChanged: function (v) {
        form.path = String(v)
        // The old operator may not be accepted by the new path.
        form.operator = form.operators.length > 0 ? String(form.operators[0]) : ""
      }
      // Focus goes back to the window when a popup closes, not to the control.
      onPopupOpenChanged: if (!popupOpen) Qt.callLater(function () { pathMenu.forceActiveFocus() })
    }
  }

  Labelled {
    label: "WHEN IT"
    visible: form.operators.length > 0

    ButtonGroup {
      id: opMenu
      foreground: form.foreground
      accent: Color.accent
      fontFamily: form.fontFamily
      options: {
        var out = []
        for (var i = 0; i < form.operators.length; i++)
          out.push({ value: String(form.operators[i]), label: String(form.operators[i]) })
        return out
      }
      value: form.operator
      onChanged: function (v) { form.operator = String(v) }
      readonly property bool popupOpen: false
    }
  }

  Labelled {
    label: "BOUND"
    visible: form.wantsBound

    Row {
      spacing: Style.space(8)

      ButtonGroup {
        id: aboveBelow
        foreground: form.foreground
        accent: Color.accent
        fontFamily: form.fontFamily
        options: [
          { value: "above", label: "above" },
          { value: "below", label: "below" }
        ]
        value: "below"
      }

      TextField {
        id: boundField
        width: Style.space(90)
        placeholderText: "10"
        foreground: form.foreground
        accent: Color.accent
        font.family: form.fontFamily
        validator: DoubleValidator { }
        Keys.onEscapePressed: form.cancelled()
        onAccepted: form.submit()
      }
    }
  }

  Labelled {
    label: "VALUE"
    visible: form.wantsValue

    TextField {
      id: valueField
      width: form.width
      placeholderText: "true"
      foreground: form.foreground
      accent: Color.accent
      font.family: form.fontFamily
      Keys.onEscapePressed: form.cancelled()
      onAccepted: form.submit()
    }
  }

  Labelled {
    label: "OLDER THAN"
    visible: form.wantsAge

    TextField {
      id: ageField
      width: form.width
      placeholderText: "2h"
      foreground: form.foreground
      accent: Color.accent
      font.family: form.fontFamily
      Keys.onEscapePressed: form.cancelled()
      onAccepted: form.submit()
    }
  }

  Labelled {
    label: "EXPIRES"
    hint: "How long the watch stands: 90m, 12h, 7d, or a date."

    TextField {
      id: expiresField
      width: form.width
      text: "30d"
      foreground: form.foreground
      accent: Color.accent
      font.family: form.fontFamily
      Keys.onEscapePressed: form.cancelled()
      onAccepted: form.submit()
    }
  }

  Labelled {
    label: "REASON"
    hint: "The alarm carries this and nothing else, so write it for whoever is woken."

    TextField {
      id: reasonField
      width: form.width
      placeholderText: "why this matters"
      foreground: form.foreground
      accent: Color.accent
      font.family: form.fontFamily
      Keys.onEscapePressed: form.cancelled()
      onAccepted: form.submit()
    }
  }

  Toggle {
    id: standing
    width: form.width
    label: "Ring every time, not just once"
    checked: false
    foreground: form.foreground
    accent: Color.accent
    fontFamily: form.fontFamily
    // Toggle is stateless about the value and emits clicked(), not toggled().
    onClicked: standing.checked = !standing.checked
  }

  Row {
    spacing: Style.space(8)

    Button {
      id: saveButton
      text: "Arm it"
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

  // Explicit ring rather than activeFocusOnTab order.
  Component.onCompleted: {
    pathMenu.KeyNavigation.tab = opMenu
    opMenu.KeyNavigation.tab = expiresField
    expiresField.KeyNavigation.tab = reasonField
    reasonField.KeyNavigation.tab = saveButton
    saveButton.KeyNavigation.tab = cancelButton
    cancelButton.KeyNavigation.tab = pathMenu

    opMenu.KeyNavigation.backtab = pathMenu
    expiresField.KeyNavigation.backtab = opMenu
    reasonField.KeyNavigation.backtab = expiresField
    saveButton.KeyNavigation.backtab = reasonField
    cancelButton.KeyNavigation.backtab = saveButton
    pathMenu.KeyNavigation.backtab = cancelButton
  }
}
