# vhs tape — regenerate the headline asciinema with:
#   vhs docs/demo.tape  (produces docs/demo.gif)
#   asciinema rec docs/demo.cast -c "scripts/demo.sh"
#
# vhs install: https://github.com/charmbracelet/vhs

Output docs/demo.gif

Set Theme "Catppuccin Mocha"
Set FontFamily "JetBrains Mono"
Set FontSize 16
Set Width 1200
Set Height 700
Set Padding 18
Set TypingSpeed 30ms

Type "excise"
Enter
Sleep 800ms

# pretend the picker is open; tap down a few times
Down 4
Sleep 300ms
Space@1s

Down 1
Space@1s
Down 1
Space@1s

Sleep 1s
Enter            # commit
Sleep 200ms
Type "y"
Enter
Sleep 1.5s
