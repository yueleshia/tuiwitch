// The entrypoint, this handles CLI parsing and the TUI
package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	//"time"
	"os"

	"github.com/yueleshia/tuiwitch/src"
	"github.com/yueleshia/tuiwitch/src/tui"
)

func help() {
		fmt.Println(`
Possible options:
USAGE: (Use first character or full word)

tuiwitch follow                    - list online status of various channels
tuiwitch open <channel> [<offset>] - see latest vods
tuiwitch vods <channel> [<offset>] - see latest vods
`)
}

//go:embed channel_list.txt
var CHANNELS string

//run: go run % f

var STDIN_FD int

var CACHE = src.RingBuffer {
	// You typically want 70% fullness for hash maps
	// Although we wrap around (so we could add up to 2 * RING_QUEUE_SIZE)
	// before deleting elements, we typically typically are only adding vidoes
	// 10 at a time, so 1.3 * BUFFER_SIZE would be enough
	Latest: make(map[string]src.Video, src.RING_QUEUE_SIZE * 2),
	Buffer: make([]src.Video, src.RING_QUEUE_SIZE),
}
var UI = tui.UIState{}

// Plumbing (low-level) and porcelain (user-facing) are GIT developer terminology
func main() {
	src.Set_log_level(src.DEBUG)

	{
		list := make([]string, len(os.Args[1:]))
		for i, arg := range os.Args[1:] {
			list[i] = fmt.Sprintf("%q", arg)
		}
		src.L_DEBUG.Printf("Args: %s\n", strings.Join(list, " "))
	}

	var cmd string
	if len(os.Args[1:]) <= 0 {
		cmd = "interactive"
	} else {
		cmd = os.Args[1]
	}

	UI.Load_follow_list(CHANNELS, &CACHE)
	UI.Follow_videos = make([]src.Video, len(UI.Channel_list))

	switch cmd {
	case "interactive":
		STDIN_FD = int(os.Stdin.Fd())
		//interactive()
		
	case "o": fallthrough
	case "open":
		// @TODO: test behaviour on VOD
		var channel string
		if len(os.Args) >= 3 {
			channel = os.Args[2]
		}

		if strings.ContainsAny(channel, "/") {
			fmt.Fprintf(os.Stderr, "Invalid channel name %q", channel)
			return
		}
		CACHE.Query_channel(channel)

		cur := src.Video{}
		buffer_length := len(CACHE.Buffer)
		for i := CACHE.Start; i < CACHE.Close; i += 1 {
			vid := CACHE.Buffer[i % buffer_length]
			if vid.Start_time.After(cur.Start_time) {
				cur = vid
			}
		}

		play(cur)

	case "f": fallthrough
	case "follow":
		var wg sync.WaitGroup
		wg.Add(len(UI.Channel_list))
		for _, channel := range UI.Channel_list {
			go func() {
				CACHE.Query_channel(channel)
				wg.Done()
			}()
		}
		wg.Wait()

		//buffer_length := len(CACHE.Buffer)
		idx := 0
		for _, vid := range CACHE.Latest {
			UI.Follow_videos[idx] = vid
			idx += 1
		}

		slices.SortFunc(UI.Follow_videos, func(a, b src.Video) int {
			less_than := false
			if a.Is_live && b.Is_live {
				// Shortest live first
				less_than = a.Duration < b.Duration
			} else if a.Is_live || b.Is_live {
				less_than = a.Is_live // Live first, vod after
			} else {
				a_close := a.Start_time.Add(a.Duration)
				b_close := b.Start_time.Add(b.Duration)
				// Latest close time first
				less_than = a_close.After(b_close)
			}
			if less_than {
				return -1
			} else {
				return 0
			}
		})

		choice, err := basic_menu(
			"Follow list\n",
			len(UI.Follow_videos),
			"Enter a Video: ",
			func (out io.Writer, idx int) {
				tui.Print_formatted_line(out, " | ", UI.Follow_videos[idx])
			},
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			return
		}
		play(UI.Follow_videos[choice])

	case "v": fallthrough
	case "vods":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Please specify a channel to query the VODs for")
			os.Exit(1)
		}
		channel := os.Args[2]
		CACHE.Query_channel(channel)

		buffer_length := len(CACHE.Buffer)
		choice, err := basic_menu(
			fmt.Sprintf("VODs for %s\n", channel),
			CACHE.Close - CACHE.Start,
			"Enter a Video: ",
			func (out io.Writer, idx int) {
				vid := CACHE.Buffer[(CACHE.Start + idx) % buffer_length]
				tui.Print_formatted_line(out, " | ", vid)
			},
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			return
		}

		vid := CACHE.Buffer[(CACHE.Start + choice) % buffer_length]
		play(vid)

		//var list strings.Builder
		//buffer_length := len(CACHE.Buffer)
		//for i := CACHE.Start; i < CACHE.Close; i += 1 {
		//	vid := CACHE.Buffer[i % buffer_length]
		//	tui.Print_formatted_line(&list, " | ", vid)
		//}

	default:
		fmt.Fprintf(os.Stderr, "Unsupported command %q\n", cmd)
	}
}

func play(vid src.Video) {
	tui.Print_formatted_line(os.Stderr, " | ", vid)
	stdin := bufio.NewReader(os.Stdin)
	if vid.Is_live {
		fmt.Fprint(os.Stderr, "Start time (e.g. 1:00:00) (leave blank for live): ")
	} else {
		fmt.Fprint(os.Stderr, "Start time (e.g. 1:00:00): ")
	}

	//src.Must1(src.Run(nil, os.Stdout, "streamlink", "https://www.twitch.tv/" + vid.Channel))
	var start_time string
	if input, err := stdin.ReadString('\n'); err != nil {
		fmt.Fprint(os.Stderr, "%s\n", err)
	} else {
		start_time = input[:len(input) - len("\n")]
	}

	if start_time == "" {
		if vid.Is_live {
			src.Must1(src.Run(nil, os.Stdout, "streamlink", "https://www.twitch.tv/" + vid.Channel))
		} else {
			src.Must1(src.Run(nil, os.Stdout, "streamlink", vid.Url))
		}
	} else {
		src.Must1(src.Run(nil, os.Stdout, "streamlink", "--hls-start-offset", start_time, vid.Url))
	}
}


func basic_menu(header string, option_count int, prompt string, print_option func (io.Writer, int)) (int, error) {
	if option_count == 0 {
		return -1, fmt.Errorf("No options available")
	} else if option_count == 1 {
		return 0, nil
	}

	max_index_digit_count := strconv.Itoa(len(strconv.Itoa(option_count)))
	for {
		fmt.Fprintf(os.Stderr, "%s", header)
		for i := 0; i < option_count; i += 1 {
			fmt.Fprintf(os.Stderr, "%" + max_index_digit_count + "d | ", i + 1)
			print_option(os.Stderr, i)
		}

		fmt.Fprintf(os.Stderr, "(Press Ctrl-c to quit)\n%s", prompt)
		stdin := bufio.NewReader(os.Stdin)

		var choice_as_str string
		if input, err := stdin.ReadString('\n'); err != nil {
			return -1, err
		} else {
			choice_as_str = input[:len(input) - len("\n")]
		}

		if choice, err := strconv.Atoi(choice_as_str); err != nil {
			fmt.Fprintf(os.Stderr, "%s%q is not a number%s\n", src.ANSI_FG_RED, choice_as_str, src.ANSI_RESET)
			continue
		} else if choice < 1 || choice > option_count {
			fmt.Fprintf(os.Stderr, "%s%q is not one of the options%s\n", src.ANSI_FG_RED, choice_as_str, src.ANSI_RESET)
		} else {
			return choice - 1, nil
		}
	}
}
