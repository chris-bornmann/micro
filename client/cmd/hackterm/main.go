// Command hackterm plays the game in a terminal.
//
// It exists for two reasons beyond being playable. It is the smallest possible
// client, so it shows exactly how little a client needs to know: it draws
// characters it cannot interpret and forwards keys it does not understand. And
// it links no GUI toolkit and no font, so it starts instantly and debugs
// pleasantly, which the windowed client does not.
//
// Input is line-based rather than raw-mode: type a command and press Enter.
// That keeps this file free of terminal-mode handling, and makes a session
// scriptable by piping keys in — which is how its own test drives it.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"micro/client/internal/ansi"
	"micro/client/internal/httpclient"
)

// keyFor translates what a person types into a protocol key name. The long
// names exist because a terminal has no arrow-key events in line mode, and
// because "up" is easier to type than "Up".
//
// Anything not listed is passed through unchanged, so a single letter like "A"
// or "D" reaches the server as itself, and so an unrecognized key still
// produces the server's own response rather than a client-side complaint.
var keyFor = map[string]string{
	"h": "Left", "j": "Down", "k": "Up", "l": "Right",
	"left": "Left", "down": "Down", "up": "Up", "right": "Right",
	"wait": ".", "get": ",", "up-stairs": "<", "down-stairs": ">",
	"esc": "Escape", "escape": "Escape",
}

// viewFor maps a typed word to one of the server's read-only views.
var viewFor = map[string]string{
	"i": "inventory", "inv": "inventory", "inventory": "inventory",
	"look": "look", "here": "look",
	"s": "status", "status": "status",
	"?": "help", "help": "help",
}

func main() {
	url := flag.String("url", "http://localhost:8080", "server address")
	seed := flag.Int64("seed", 0, "seed for a reproducible opening state (0 = let the server choose)")
	wait := flag.Duration("wait", 5*time.Second, "how long to wait for the server to come up")
	flag.Parse()

	c := httpclient.New(*url)
	if err := c.WaitForServer(*wait); err != nil {
		fmt.Fprintln(os.Stderr, "hackterm:", err)
		fmt.Fprintln(os.Stderr, "hackterm: is the server running? try: go run ./cmd/mapd")
		os.Exit(1)
	}

	frame, err := c.NewGame(seed)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hackterm:", err)
		os.Exit(1)
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	render(out, frame, nil, "")
	out.Flush()

	in := bufio.NewScanner(os.Stdin)
	// The open prompt's kind, if any: a lone letter answers "choose"; a
	// lone character, exactly as typed, answers "character".
	var prompt httpclient.PromptKind
	for {
		fmt.Fprint(out, "\n> ")
		out.Flush()

		if !in.Scan() {
			break // EOF: piped input ran out, or the user pressed ctrl-D
		}
		word := strings.TrimSpace(in.Text())
		if word == "" {
			continue
		}
		key, answered := "", false
		switch prompt {
		case httpclient.PromptChoose:
			key, answered = pickLetter(word)
		case httpclient.PromptCharacter:
			key, answered = word, utf8.RuneCountInString(word) == 1
		}
		if !answered && (word == "quit" || word == "q") {
			break
		}

		// A read-only view costs no turn, so it does not go through Command.
		if name, ok := viewFor[strings.ToLower(word)]; ok && !answered {
			v, err := c.View(name)
			if err != nil {
				fmt.Fprintln(out, "error:", err)
				continue
			}
			fmt.Fprintf(out, "\n%s\n%s\n", v.Title, strings.Repeat("-", len(v.Title)))
			for _, line := range v.Lines {
				fmt.Fprintln(out, " ", line)
			}
			continue
		}

		if !answered {
			key = word
			if mapped, ok := keyFor[strings.ToLower(word)]; ok {
				key = mapped
			}
		}

		resp, err := c.Command(key)
		if err != nil {
			fmt.Fprintln(out, "error:", err)
			continue
		}
		prompt = ""
		if resp.Prompt != nil {
			prompt = resp.Prompt.Kind
		}

		// A prompt means no turn passed, so there is no new frame and the
		// board on screen is still current. Show the prompt and wait for the
		// next key, which the server will interpret as an answer.
		if resp.Prompt != nil {
			fmt.Fprintf(out, "\n%s\n", resp.Prompt.Title)
			for _, line := range resp.Prompt.Lines {
				fmt.Fprintln(out, " ", line)
			}
			continue
		}

		if resp.Frame == nil {
			// Nothing resolved and nothing opened; ask for the board back.
			f, ferr := c.Frame()
			if ferr != nil {
				fmt.Fprintln(out, "error:", ferr)
				continue
			}
			resp.Frame = &f
		}

		render(out, *resp.Frame, resp.Messages, key)
		if resp.Frame.GameOver {
			fmt.Fprintln(out, "\nGame over.")
			break
		}
	}
}

// pickLetter reads word as the answer to a "choose" prompt: any single
// letter, in either case, is sent upper-cased as the item letter it names —
// even one that is also a shortcut, like "h" or "i" or "q". A letter no
// item has reaches the server, which closes the prompt.
func pickLetter(word string) (string, bool) {
	if len(word) == 1 && ('a' <= word[0] && word[0] <= 'z' || 'A' <= word[0] && word[0] <= 'Z') {
		return strings.ToUpper(word), true
	}
	return "", false
}

// render draws one frame: the board, the HUD, and whatever the turn had to say.
//
// Everything here is either a character to paint or a string to print. There is
// no branch in this function that depends on what anything in the game IS,
// which is the whole point of the boundary.
func render(w io.Writer, f httpclient.Frame, messages []string, key string) {
	grid, err := f.Decode()
	if err != nil {
		fmt.Fprintln(w, "error: malformed frame:", err)
		return
	}

	fmt.Fprintf(w, "\n%s   (turn %d)\n\n", f.Title, f.Turn)
	fmt.Fprint(w, ansi.Render(grid))
	if len(f.HUD) > 0 {
		fmt.Fprintf(w, "\n%s\n", strings.Join(f.HUD, "   "))
	}
	if len(messages) > 0 {
		for _, m := range messages {
			fmt.Fprintln(w, " *", m)
		}
	}
}
