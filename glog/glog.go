package glog

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httputil"
	"os"
	"reflect"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

type Logger struct {
	logger *zap.SugaredLogger
}

func timeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("060102-150405.000"))
}

type Option struct {
	IsEncoderJson bool
	IsSaveFile    bool
	FileName      string
	MaxSize       int
	MaxAge        int
	Compress      bool
}

func OutputCoreEncoder(option Option, cfg zapcore.EncoderConfig) zapcore.Encoder {
	if option.IsEncoderJson {
		return zapcore.NewJSONEncoder(cfg) //json格式
	} else {
		return NewMyConsoleEncoder(cfg) //自定义格式
	}
}

func New(option Option) *Logger {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = timeEncoder
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	encoderConfig.EncodeDuration = zapcore.SecondsDurationEncoder
	// encoderConfig.EncodeLevel = capitalLevelEncoder
	// encoderConfig.EncodeLevel = zapcore.LowercaseColorLevelEncoder //debug等级显示颜色
	var writers []zapcore.WriteSyncer
	writers = append(writers, os.Stdout)
	if option.IsSaveFile {
		hook := lumberjack.Logger{
			Filename: option.FileName,
			MaxSize:  option.MaxSize,
			MaxAge:   option.MaxAge,
			Compress: option.Compress,
		}
		writers = append(writers, zapcore.AddSync(&hook))
	}
	atomicLevel := zap.NewAtomicLevel()
	atomicLevel.SetLevel(zap.DebugLevel)
	core := zapcore.NewCore(
		OutputCoreEncoder(option, encoderConfig),
		zapcore.NewMultiWriteSyncer(writers...),
		atomicLevel,
	)
	caller := zap.AddCaller()
	development := zap.Development()
	return &Logger{
		logger: zap.New(core, caller, development, zap.AddCallerSkip(1)).Sugar(),
	}
}

func (l *Logger) Debug(args ...interface{}) {
	l.logger.Debug(args...)
}

func (l *Logger) Info(args ...interface{}) {
	l.logger.Info(args...)
}

func (l *Logger) Warn(args ...interface{}) {
	l.logger.Warn(args...)
}

func (l *Logger) Error(args ...interface{}) {
	l.logger.Error(args...)
}

func (l *Logger) Println(args ...interface{}) {
	l.logger.Debug(args...)
}

func (l *Logger) Printf(f string, a ...interface{}) {
	l.logger.Debug(fmt.Sprintf(f, a...))
}

func (l *Logger) Type(args ...interface{}) {
	for _, a := range args {
		l.Debug(reflect.TypeOf(a))
	}
}
func (l *Logger) Dump(b []byte) {
	l.logger.Debug("\r\n" + hex.Dump(b))
}

func (l *Logger) Struct(args interface{}) {
	m := fmt.Sprintf("%T : %+v", args, args)
	l.logger.Debug(m)
}

func (l *Logger) Request(r *http.Request, body bool) {
	s, _ := httputil.DumpRequest(r, body)
	l.Debug("Request-> " + r.URL.Host + " \r\n" + string(s))
}

func (l *Logger) Response(r *http.Response, body bool) {
	s, _ := httputil.DumpResponse(r, body)
	l.Debug("Response -> \r\n" + string(s))
}
