#!/usr/bin/env nu

# Drives `ghst demo` in a private tmux server and screenshots it in full
# color with freeze. Needs the dev shell for tmux, freeze, and $GHST_SHOT_FONT.

const socket = "ghst-shot"
const out = "tmp/shots"

def --wrapped t [...args: string] {
  ^tmux -L $socket -f /dev/null ...$args
}

# Builds the demo and starts it at the given size.
def "main start" [cols: int = 80 rows: int = 50] {
  mkdir $out
  go build -o $"($out)/ghst" .
  try { t kill-server o+e>| ignore }
  # oxtabs (tab3 on Linux) stops the renderer from moving the cursor with
  # hard tabs, which tmux keeps in captures.
  let bin = ($"($out)/ghst" | path expand)
  let cmd = $"stty oxtabs 2>/dev/null || stty tab3; TERM=xterm-256color COLORTERM=truecolor exec ($bin) demo"
  t new-session -d -s demo -x ($cols | into string) -y ($rows | into string) -e TMUX= $cmd
  sleep 1.5sec
}

# Sends tmux keys in order, such as j, Enter, Escape, Space, or C-c.
def "main keys" [...keys: string] {
  for k in $keys {
    t send-keys -t demo $k
    sleep 150ms
  }
  sleep 400ms
}

# Writes tmp/shots/<name>.png and prints its path.
def "main shot" [name: string] {
  let png = $"($out)/($name).png"
  # freeze ignores the default background, so set the base color instead.
  let screen = t capture-pane -e -p -t demo | str replace -a "\e[49m" $"\e[48;2;30;30;46m"
  # tmux sets colors only where they change, but freeze resets them at each
  # line, so start every line with the foreground still in effect. Carried
  # backgrounds would paint the padding.
  mut state = ""
  mut fixed = []
  for line in ($screen | lines) {
    $fixed = ($fixed | append $"($state)($line)")
    for seq in ($line | parse -r '(?<s>\x1b\[[0-9;:]*m)' | get s) {
      $state = if $seq in ["\e[0m" "\e[m"] {
        ""
      } else if ($seq | str starts-with "\e[48") {
        $state
      } else {
        $state + $seq
      }
    }
  }
  $fixed | str join "\n" | save -f $"($out)/($name).ansi"
  let args = [
    "-l"
    "ansi"
    "--font.file"
    $env.GHST_SHOT_FONT
    "--font.size"
    "14"
    "--background"
    "#1e1e2e"
    "--padding"
    "16"
    "--margin"
    "0"
    "--border.radius"
    "0"
    "--window=false"
    "-o"
    $png
  ]
  open --raw $"($out)/($name).ansi" | ^freeze ...$args | ignore
  $png | path expand
}

# Prints the screen as plain text.
def "main text" [] {
  t capture-pane -p -t demo
}

def "main resize" [cols: int rows: int] {
  t resize-window -t demo -x ($cols | into string) -y ($rows | into string)
  sleep 500ms
}

def "main stop" [] {
  t kill-server
}

def main [] {
  help main
}
