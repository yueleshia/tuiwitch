package src

import (
	"time"
)

type Result[T any] struct {
	Val T
	Err error
}

func Is_similar_time(a, b time.Time) bool {
	delta := a.Sub(b)
	return -5 * time.Minute < delta && delta < 5 * time.Minute
}

////////////////////////////////////////////////////////////////////////////////

// @TODO: Check if using a BTreeMap would be faster than a ring buffer
//        This is relevant for the channel list in interactive

// Our basic algorithm is start..close are valid index
// Once close has passed len(Buffer), it will always be len + idx, thus close is
// always a > start. It is on the usage code to do a modulus.
type RingBuffer struct {
	Latest map[string]Video
	Buffer []Video
	Start int
	Close int
	Wrapped bool
}
func (self *RingBuffer) Add(items []Video) {
	length := len(self.Buffer)

	//if self.Close >= length {
	//	for i := 0; i < len(items); i += 1 {
	//		delete(self.Latest, self.Buffer[(self.Close + i) % length].Url)
	//	}
	//}
	for _, vid := range items {
		// If one of the channels we follow
		if las, ok := self.Latest[vid.Channel]; ok {
			vid_close_time := vid.Start_time.Add(vid.Duration)
			las_close_time := las.Start_time.Add(las.Duration)

			if vid.Is_live {
				self.Latest[vid.Channel] = vid
			} else if Is_similar_time(vid.Start_time, las.Start_time) {
				vid.Is_live = las.Is_live
				self.Latest[vid.Channel] = vid
			} else if vid_close_time.After(las_close_time) {
				self.Latest[vid.Channel] = vid
			}
		}

		//if _, ok := self.Latest[vid.Url]; ok {
		//	continue
		//} else {
		//	self.Latest[vid.Url] = vid
		//}
		self.Buffer[self.Close % length] = vid
		self.Close += 1
	}
	if self.Close > length {
		self.Close = (self.Close % length) + length
		self.Start = self.Close
	}
	self.Start %= length
}

func (self *RingBuffer) As_slice() []Video {
	if self.Close <= len(self.Buffer) {
		return self.Buffer[:self.Close]
	} else {
		return self.Buffer
	}
}
