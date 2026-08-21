# CodeTree demo: neighborhood focus mode
Output docs/focus.gif

Set Shell "bash"
Set FontSize 15
Set Width 1200
Set Height 750
Set Padding 16
Set Framerate 30
Set Theme "Catppuccin Mocha"

Type "cd ~/dev/ct-samples/pyml"
Enter
Sleep 300ms
Type "ct"
Enter
Sleep 3s

# mark networks.py and open its diagram
Type "jjj"
Sleep 400ms
Space
Sleep 500ms
Enter
Sleep 1500ms

# walk to ResNet, then focus its neighborhood
Type "jll"
Sleep 800ms
Enter
Sleep 1300ms

# deeper descendants, then include siblings
Type "+"
Sleep 900ms
Type "b"
Sleep 1200ms

Escape
Sleep 600ms
Type "q"
Sleep 400ms
