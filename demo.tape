# CodeTree demo: picker → mark files → class diagram → navigate → members
Output demo.gif

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

# picker: move and mark two files
Type "jj"
Sleep 400ms
Space
Sleep 400ms
Type "j"
Sleep 300ms
Space
Sleep 600ms
Enter
Sleep 1500ms

# diagram: navigate the forest, expand member compartments
Type "jjll"
Sleep 900ms
Type "m"
Sleep 1200ms

Type "q"
Sleep 500ms
