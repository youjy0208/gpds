package main

import "github.com/youjy0208/gpds/glog"

func main() {
	optione := glog.Option{
		IsEncoderJson: true,
		IsSaveFile:    true,
		FileName:      "log.txt",
		MaxSize:       1024,
		Compress:      true,
	}
	filelog := glog.New(optione)
	filelog.Debug("123")
}
