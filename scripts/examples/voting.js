// voting.js — Voting Booth
//
// Displays a poll question and lets users vote. Results persist via v3.data.
// Pass the topic ID and question as script arguments:
//
// Configure in doors.json:
//   { "name": "VOTING", "type": "v3_script", "script": "voting.js",
//     "working_directory": "scripts/examples",
//     "args": ["poll1", "What is the best BBS software?", "Vision/3", "Mystic", "Synchronet", "ENiGMA½"] }

var topicID = v3.args[0] || "default";
var question = v3.args[1] || "What do you think?";

// Build choices from remaining args.
var choices = [];
for (var i = 2; i < v3.args.length; i++) {
    choices.push(v3.args[i]);
}
if (choices.length === 0) {
    choices = ["Yes", "No", "Maybe"];
}

function loadPoll() {
    var poll = v3.data.get("poll_" + topicID);
    if (!poll) {
        poll = { votes: {}, voters: {} };
        for (var i = 0; i < choices.length; i++) {
            poll.votes[choices[i]] = 0;
        }
    }
    return poll;
}

function savePoll(poll) {
    v3.data.set("poll_" + topicID, poll);
}

function displayResults(poll) {
    v3.console.println("");
    v3.console.println("|11--- Results ---");
    var total = 0;
    for (var i = 0; i < choices.length; i++) {
        total += (poll.votes[choices[i]] || 0);
    }
    for (var i = 0; i < choices.length; i++) {
        var count = poll.votes[choices[i]] || 0;
        var pct = total > 0 ? Math.round((count / total) * 100) : 0;
        var bar = v3.util.padRight("", Math.round(pct / 5), "#");
        v3.console.println("|07" + v3.util.padRight(choices[i], 20) + " |15" + v3.util.padRight(bar, 20) + " |07" + count + " (" + pct + "%)");
    }
    v3.console.println("|08Total votes: " + total);
}

v3.console.clear();
v3.console.println("|09=== |15Voting Booth |09===");
v3.console.println("");
v3.console.println("|11" + question);
v3.console.println("");

var poll = loadPoll();

// Check if user already voted.
if (poll.voters[v3.user.handle]) {
    v3.console.println("|07You already voted in this poll.");
    displayResults(poll);
    v3.console.pause();
    exit();
}

// Display choices.
for (var i = 0; i < choices.length; i++) {
    v3.console.println("|15" + (i + 1) + "|07. " + choices[i]);
}
v3.console.println("");
v3.console.print("|07Your choice (1-" + choices.length + "): ");
var pick = v3.console.getnum(choices.length);

if (pick >= 1 && pick <= choices.length) {
    var chosen = choices[pick - 1];
    poll.votes[chosen] = (poll.votes[chosen] || 0) + 1;
    poll.voters[v3.user.handle] = true;
    savePoll(poll);
    v3.console.println("");
    v3.console.println("|10Vote recorded: |15" + chosen);
}

displayResults(poll);
v3.console.pause();
