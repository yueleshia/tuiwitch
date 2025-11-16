// The entrypoint, this handles CLI parsing and the TUI
package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"os"
	"os/exec"

	"github.com/rivo/uniseg"
	//"golang.org/x/term"

	"github.com/yueleshia/tuiwitch/src"
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

// @TODO: Account for \r
var CHANNEL_LIST = func () []string {
	list := strings.Split(CHANNELS, "\n")
	idx := 0
	// filter out empty string and trim "\r"
	for _, x := range list {
		list[idx], _ = strings.CutSuffix(x, "\r")
		if "" != list[idx] {
			idx += 1
		}
	}
	return list[:idx]
}()

//run: go run % f

// Plumbing (low-level) and porcelain (user-facing) are GIT developer terminology
func main() {
	src.Set_log_level(src.DEBUG)

	// @TODO: Refactor this to work even when we run out of cache
	src.Assert(len(CHANNEL_LIST) * src.PAGE_SIZE <= src.RING_QUEUE_SIZE)

	{
		list := make([]string, len(os.Args[1:]))
		for i, arg := range os.Args[1:] {
			list[i] = fmt.Sprintf("%q", arg)
		}
		src.L_DEBUG.Printf("Args: %s\n", strings.Join(list, " "))
	}
	if len(os.Args[1:]) <= 0 {
		help()
		os.Exit(0)
	}

	switch os.Args[1] {
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
		query_channel(channel)

		cur := src.Video{}
		buffer_length := len(RING_QUEUE.buffer)
		for i := RING_QUEUE.start; i < RING_QUEUE.close; i += 1 {
			vid := RING_QUEUE.buffer[i % buffer_length]
			if vid.Start_time.After(cur.Start_time) {
				cur = vid
			}
		}

		play(cur)

	case "f": fallthrough
	case "follow":
		var wg sync.WaitGroup
		wg.Add(len(CHANNEL_LIST))
		for _, channel := range CHANNEL_LIST {
			go func() {
				query_channel(channel)
				wg.Done()
			}()
		}
		wg.Wait()

		// Find latest video for each channel
		for _, channel := range CHANNEL_LIST {
			UI_MAP[channel] = src.Video{
				Channel: channel,
			}
		}
		buffer_length := len(RING_QUEUE.buffer)
		for i := RING_QUEUE.start; i < RING_QUEUE.close; i += 1 {
			vid := RING_QUEUE.buffer[i % buffer_length]
			cur := UI_MAP[vid.Channel]

			if vid.Is_live {
				src.Assert(!cur.Is_live)
				UI_MAP[vid.Channel] = vid
			} else if vid.Start_time.After(cur.Start_time) {
				UI_MAP[vid.Channel] = vid
			}
		}

		choice, err := basic_menu(
			"Follow list\n",
			len(CHANNEL_LIST),
			"Enter a Video: ",
			func (out io.Writer, idx int) {
				print_formatted_line(out, " | ", UI_MAP[CHANNEL_LIST[idx]])
			},
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			return
		}

		vid := UI_MAP[CHANNEL_LIST[choice]]
		play(vid)
		//choose(strings.NewReader(list.String()))

	case "v": fallthrough
	case "vods":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Please specify a channel to query the VODs for")
			os.Exit(1)
		}
		channel := os.Args[2]
		query_channel(channel)

		buffer_length := len(RING_QUEUE.buffer)
		choice, err := basic_menu(
			fmt.Sprintf("VODs for %s\n", channel),
			RING_QUEUE.close - RING_QUEUE.start,
			"Enter a Video: ",
			func (out io.Writer, idx int) {
				vid := RING_QUEUE.buffer[(RING_QUEUE.start + idx) % buffer_length]
				print_formatted_line(out, " | ", vid)
			},
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			return
		}

		vid := RING_QUEUE.buffer[(RING_QUEUE.start + choice) % buffer_length]
		play(vid)

		//var list strings.Builder
		//buffer_length := len(RING_QUEUE.buffer)
		//for i := RING_QUEUE.start; i < RING_QUEUE.close; i += 1 {
		//	vid := RING_QUEUE.buffer[i % buffer_length]
		//	print_formatted_line(&list, " | ", vid)
		//}

	default:
		fmt.Fprintf(os.Stderr, "Unsupported command %q\n", os.Args[1])
	}
}

func play(vid src.Video) {
	print_formatted_line(os.Stderr, " | ", vid)
	stdin := bufio.NewReader(os.Stdin)
	if vid.Is_live {
		fmt.Fprint(os.Stderr, "Start time (e.g. 1:00:00) (leave blank for live): ")
	} else {
		fmt.Fprint(os.Stderr, "Start time (e.g. 1:00:00): ")
	}

	//src.Must1(run(nil, os.Stdout, "streamlink", "https://www.twitch.tv/" + vid.Channel))
	var start_time string
	if input, err := stdin.ReadString('\n'); err != nil {
		fmt.Fprint(os.Stderr, "%s\n", err)
	} else {
		start_time = input[:len(input) - len("\n")]
	}

	if start_time == "" {
		if vid.Is_live {
			src.Must1(run(nil, os.Stdout, "streamlink", "https://www.twitch.tv/" + vid.Channel))
		} else {
			src.Must1(run(nil, os.Stdout, "streamlink", vid.Url))
		}
	} else {
		src.Must1(run(nil, os.Stdout, "streamlink", "--hls-start-offset", start_time, vid.Url))
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

var UI_MAP = make(map[string]src.Video, len(CHANNEL_LIST) * 2)
var UI_LIST = make([]src.Video, src.RING_QUEUE_SIZE)

func run(input io.Reader, output io.Writer, name string, arg ...string) error {
	// The command you want to execute (for example, "cat" which echoes input)
	src.L_DEBUG.Printf("%s %s", name, strings.Join(arg, " "))
	cmd := exec.Command(name, arg...)

	cmd.Stdin = input
	cmd.Stderr = os.Stderr
	cmd.Stdout = output

	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func query_channel(channel string) {
	var vods []src.Video
	var live src.Video

	jobs := [] func() {
		func () {
			if vids, err := src.Graph_vods(channel); err != nil {
				//fmt.Println(err)
				src.L_DEBUG.Println(err)
				// @TODO: display error
			} else {
				vods = vids
			}
		},
		func () {
			if x, err := src.Scrape_live_status(channel); err != nil {
				//fmt.Println(err)
				src.L_DEBUG.Println(err)
				// @TODO: display error
			} else {
				live = x
			}
		},
	}

	var wg sync.WaitGroup
	wg.Add(len(jobs))
	for _, x := range jobs {
		go func () {
			x()
			wg.Done()
		}()
	}
	wg.Wait()

	is_found := false
	if live.Is_live { // Not live is marked by Video{}, which is defualt false
		for i := len(vods) - 1; i >= 0; i += 1 {
			delta := vods[i].Start_time.Sub(live.Start_time)
			if -5 * time.Minute < delta &&  delta < 5 * time.Minute {
				vods[i].Is_live = true
				is_found = true
				break
			}
		}
	}

	RING_QUEUE.mutex.Lock()
	RING_QUEUE.Add(vods)
	if !is_found && live.Is_live {
		RING_QUEUE.Add([]src.Video{live})
	}
	RING_QUEUE.mutex.Unlock()
}

func print_formatted_line(output io.Writer, gap string, video src.Video) {
	sizes := []int{10, 30, 9, 6}

	var s_ago, duration string
	t_ago := time.Now().Sub(video.Start_time)

	if video.Is_live {
		s_ago = "○"
		duration = fmt.Sprintf("%dh%02dm", int(t_ago.Hours()), int(t_ago.Minutes()) % 60)
	} else {
		// @NOTE twitch streams are capped at 48 hours
		if int(t_ago.Minutes()) < 100 {
			s_ago = fmt.Sprintf("%d min ago", int(t_ago.Minutes()))
		} else if int(t_ago.Hours()) < 72 {
			s_ago = fmt.Sprintf("%d hr ago", int(t_ago.Hours()))
		} else {
			s_ago = fmt.Sprintf("%d d ago", int(t_ago.Hours() / 24))
		}

		duration = fmt.Sprintf("%dh%02dm", int(video.Duration.Hours()), int(video.Duration.Minutes()) % 60)
	}
	print_line(output, gap, sizes, []string{video.Channel, video.Title, s_ago, duration})
}
func print_line(output io.Writer, gap string, sizes []int, cols []string) error {
	if len(sizes) != len(cols) {
		src.L_ERROR.Fatalf("Incorrect number of arguments")
	}

	for i, width := range sizes {
		str := cols[i]

		if (i != 0) {
			if _, err := io.Copy(output, strings.NewReader(gap)); err != nil {
				return err
			}
		}

		var to_print, extra string
		w := 0
		if is_ASCII(str) {
			w = len(str)
			if width < 3 {
				min_width := w
				if width < min_width {
					min_width = width
				}
				to_print = str[0:min_width]
				extra = "   "[0:3 - min_width]
			} else if w > width {
				to_print = str[0:width - 3]
				extra = "..."
			} else {
				to_print = str
			}
		} else {
			str_len := uniseg.StringWidth(str)

			if width < 3 {
				to_print, w = break_unicode_before(3, str)

			} else if str_len > width {
				to_print, w = break_unicode_before(width - 3, str)
				extra = "..."
			} else {
				to_print = str
				w = str_len
			}
		}

		if _, err := io.Copy(output, strings.NewReader(to_print)); err != nil {
			return err
		}
		if _, err := io.Copy(output, strings.NewReader(extra)); err != nil {
			return err
		}
		for i := w + len(extra); i < width; i += 1 {
			n, err := output.Write([]byte{' '})
			if n != 1 {
				return fmt.Errorf("Could not print")
			}
			if err != nil {
				return err
			}
		} 
	}
	n, err := output.Write([]byte{'\n'})
	if n != 1 {
		return fmt.Errorf("Could not print")
	}
	if err != nil {
		return err
	}
	return nil
}

func break_unicode_before(max int, str string) (string, int) {
	state := -1
	width := 0
	c := ""
	rest := str
	for len(rest) > 0 {
		var boundaries int
		c, rest, boundaries, state = uniseg.StepString(rest, state)
		to_add := boundaries >> uniseg.ShiftWidth

		if width + to_add > max {
			return str[:len(str) - len(rest) - len(c)], width
		}

		width += to_add
	}
	fmt.Print(width)
	return str[0:1], width
}

func is_ASCII(s string) bool {
    for i := 0; i < len(s); i++ {
        if s[i] > 128 {
            return false
        }
    }
    return true
}

////////////////////////////////////////////////////////////////////////////////

type RingBuffer struct {
	cache map[string]src.Video
	buffer []src.Video
	start int
	close int
	mutex sync.Mutex
	wrapped bool
}
var RING_QUEUE = RingBuffer {
	// You typically want 70% fullness for 
	// Although we wrap around (so we could add up to 2 * RING_QUEUE_SIZE)
	// before deleting elements, we typically typically are only adding vidoes
	// 10 at a time, so 1.3 * BUFFER_SIZE would be enough
	cache: make(map[string]src.Video, src.RING_QUEUE_SIZE * 2),
	buffer: make([]src.Video, src.RING_QUEUE_SIZE), 
}
func (r *RingBuffer) Add(items []src.Video) {
	length := len(r.buffer)

	if r.close >= length {
		for i := 0; i < len(items); i += 1 {
			delete(r.cache, r.buffer[(r.close + i) % length].Url)
		}
	}
	for _, vid := range items {
		if _, ok := r.cache[vid.Url]; ok {
			fmt.Println(vid.Channel, vid.Url)
			continue
		} else {
			r.cache[vid.Url] = vid
		}
		r.buffer[r.close % length] = vid
		r.close += 1
	}
	if r.close > length {
		r.close = (r.close % length) + length
		r.start = r.close
	}
	r.start %= length
}
