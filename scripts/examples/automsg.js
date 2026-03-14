// automsg.js — Auto-Message of the Day
//
// Displays the current auto-message and lets any user replace it.
// Uses v3.data for persistence.
//
// Configure in doors.json:
//   { "name": "AUTOMSG", "type": "v3_script", "script": "automsg.js",
//     "working_directory": "scripts/examples" }

var MAX_LINES = 5;
var MAX_WIDTH = 60;

function loadAutoMsg() {
    return v3.data.get("automsg");
}

function saveAutoMsg(msg) {
    v3.data.set("automsg", msg);
}

function displayAutoMsg(msg) {
    v3.console.clear();
    v3.console.println("|09=== |15Auto-Message |09===");
    v3.console.println("");

    if (!msg) {
        v3.console.println("|08  No auto-message set.");
    } else {
        v3.console.println("|03  Left by: |15" + msg.handle);
        v3.console.println("|03  Date:    |07" + msg.date);
        v3.console.println("");
        var lines = msg.text.split("\n");
        for (var i = 0; i < lines.length; i++) {
            v3.console.center("|11" + lines[i]);
        }
    }

    v3.console.println("");
    v3.console.println("|09" + v3.util.padRight("", 60, "="));
}

var msg = loadAutoMsg();
displayAutoMsg(msg);

v3.console.println("");
if (v3.console.yesno("|07Leave a new auto-message")) {
    v3.console.println("|07Enter your message (up to " + MAX_LINES + " lines, blank line to finish):");
    var lines = [];
    for (var i = 0; i < MAX_LINES; i++) {
        v3.console.print("|07" + (i + 1) + "> |15");
        var line = v3.console.getstr(MAX_WIDTH);
        if (!line || line.length === 0) break;
        lines.push(line);
    }

    if (lines.length > 0) {
        saveAutoMsg({
            handle: v3.user.handle,
            text: lines.join("\n"),
            date: v3.util.date("01/02/2006 15:04")
        });
        v3.console.println("");
        v3.console.println("|10Auto-message saved!");
    }
}

v3.console.pause();
