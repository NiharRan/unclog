# Records assets/demo.gif against a throwaway home directory, so no real paths or project names appear.
# Setup and recording, from the repo root: make build && python3 assets/make-demo-home.py && vhs assets/demo.tape
Output assets/demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1100
Set Height 900
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms
Set Framerate 12

Hide
Type "export HOME=/tmp/unclog-demo-home TMPDIR=/tmp/unclog-demo-home/tmp PATH=/tmp/unclog-demo-home/bin:$PATH PS1='$ '"
Enter
Type "clear"
Enter
Show

Type "unclog scan"
Sleep 400ms
Enter
Sleep 9s

Hide
Type "clear"
Enter
Show

Type "unclog clean --dry-run"
Sleep 400ms
Enter
Sleep 7s
