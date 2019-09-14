// package log 日志库
package log

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// TODO chef:
// - 性能优化，目前是基于系统库log实现的
// - 和系统库中的log跑个benchmark对比

var logErr = errors.New("log:fxxk")

type Logger interface {
	Debugf(format string, v ...interface{})
	Infof(format string, v ...interface{})
	Warnf(format string, v ...interface{})
	Errorf(format string, v ...interface{})

	Debug(v ...interface{})
	Info(v ...interface{})
	Warn(v ...interface{})
	Error(v ...interface{})

	// 打印错误并退出程序，日志级别为 LevelError
	FatalIfErrorNotNil(err error)

	Outputf(level Level, calldepth int, format string, v ...interface{})
	Output(level Level, calldepth int, v ...interface{})
}

type Level uint8

const (
	LevelDebug = iota
	LevelInfo
	LevelWarn
	LevelError
)

type Config struct {
	Level Level `json:"level"` // 日志级别，大于等于该级别的日志才会被输出

	// 文件输出和控制台输出可同时打开，控制台输出主要用做开发时调试，支持level彩色输出
	Filename   string `json:"filename"`     // 输出日志文件名，如果为空，则不写日志文件。可包含路径，路径不存在时，将自动创建
	IsToStdout bool   `json:"is_to_stdout"` // 是否以stdout输出到控制台

	RotateMByte int `json:"rotate_mbyte"` // 日志大小达到多少兆后翻滚，如果为0，则不翻滚
}

func New(c Config) (Logger, error) {
	var (
		fl  *log.Logger
		sl  *log.Logger
		dir string
		fp  *os.File
		err error
	)
	if c.Level < LevelDebug || c.Level > LevelError {
		return nil, logErr
	}
	if c.Filename != "" {
		dir = filepath.Dir(c.Filename)
		if err := os.MkdirAll(dir, 0777); err != nil {
			return nil, err
		}
		fp, err = os.Create(c.Filename)
		if err != nil {
			return nil, err
		}
		fl = log.New(fp, "", log.Ldate|log.Lmicroseconds)
	}
	if c.IsToStdout {
		sl = log.New(os.Stdout, "", log.Ldate|log.Lmicroseconds)
	}

	l := &logger{
		fileLogger:   fl,
		stdoutLogger: sl,
		c:            c,
		dir:          dir,
		fp:           fp,
	}
	return l, nil
}

const (
	levelDebugString = "DEBUG "
	levelInfoString  = " INFO "
	levelWarnString  = " WARN "
	levelErrorString = "ERROR "

	levelDebugColorString = "\033[22;37mDEBUG\033[0m "
	levelInfoColorString  = " \033[22;36mINFO\033[0m "
	levelWarnColorString  = " \033[22;33mWARN\033[0m "
	levelErrorColorString = "\033[22;31mERROR\033[0m "
)

var (
	levelToString = map[Level]string{
		LevelDebug: levelDebugString,
		LevelInfo:  levelInfoString,
		LevelWarn:  levelWarnString,
		LevelError: levelErrorString,
	}
	levelToColorString = map[Level]string{
		LevelDebug: levelDebugColorString,
		LevelInfo:  levelInfoColorString,
		LevelWarn:  levelWarnColorString,
		LevelError: levelErrorColorString,
	}
)

type logger struct {
	fileLogger   *log.Logger
	stdoutLogger *log.Logger
	c            Config

	dir string

	m  sync.Mutex
	fp *os.File
}

func (l *logger) Debugf(format string, v ...interface{}) {
	l.Outputf(LevelDebug, 3, format, v...)
}

func (l *logger) Infof(format string, v ...interface{}) {
	l.Outputf(LevelInfo, 3, format, v...)
}

func (l *logger) Warnf(format string, v ...interface{}) {
	l.Outputf(LevelWarn, 3, format, v...)
}

func (l *logger) Errorf(format string, v ...interface{}) {
	l.Outputf(LevelError, 3, format, v...)
}

func (l *logger) Debug(v ...interface{}) {
	l.Output(LevelDebug, 3, v...)
}

func (l *logger) Info(v ...interface{}) {
	l.Output(LevelInfo, 3, v...)
}

func (l *logger) Warn(v ...interface{}) {
	l.Output(LevelWarn, 3, v...)
}

func (l *logger) Error(v ...interface{}) {
	l.Output(LevelError, 3, v...)
}

func (l *logger) FatalIfErrorNotNil(err error) {
	if err != nil {
		l.Outputf(LevelError, 3, "fatal since error not nil. err=%+v", err)
		os.Exit(1)
	}
}

// TODO chef: Outputf 和 Output 代码重复
func (l *logger) Outputf(level Level, calldepth int, format string, v ...interface{}) {
	if l.c.Level > level {
		return
	}

	msg := fmt.Sprintf(format, v...) + shortFileSuffix(calldepth)
	if l.stdoutLogger != nil {
		_ = l.stdoutLogger.Output(calldepth, levelToColorString[level]+msg)
	}
	if l.fileLogger != nil {
		if l.c.RotateMByte > 0 {
			l.m.Lock()
			// 把写日志的操作也锁住，避免日志移走后，其他协程继续写老日志文件
			// TODO chef: 性能比较差，系统库内部也有锁
			defer l.m.Unlock()
			if fi, err := os.Stat(l.c.Filename); err == nil {
				if fi.Size() > int64(l.c.RotateMByte)*1024*1024 {
					newFileName := l.c.Filename + "." + time.Now().Format("20060102150405")
					if err := os.Rename(l.c.Filename, newFileName); err == nil {
						_ = l.fp.Close()
						l.fp, _ = os.Create(l.c.Filename)
						l.fileLogger.SetOutput(l.fp)
					}
				}
			}
		}
		_ = l.fileLogger.Output(calldepth, levelToString[level]+msg)
	}
}

func (l *logger) Output(level Level, calldepth int, v ...interface{}) {
	if l.c.Level > level {
		return
	}

	msg := fmt.Sprint(v...) + shortFileSuffix(calldepth)
	if l.stdoutLogger != nil {
		_ = l.stdoutLogger.Output(calldepth, levelToColorString[level]+msg)
	}
	if l.fileLogger != nil {
		if l.c.RotateMByte > 0 {
			l.m.Lock()
			// 把写日志的操作也锁住，避免日志移走后，其他协程继续写老日志文件
			// TODO chef: 性能比较差，系统库内部也有锁
			defer l.m.Unlock()
			if fi, err := os.Stat(l.c.Filename); err == nil {
				if fi.Size() > int64(l.c.RotateMByte)*1024*1024 {
					newFileName := l.c.Filename + "." + time.Now().Format("20060102150405")
					if err := os.Rename(l.c.Filename, newFileName); err == nil {
						_ = l.fp.Close()
						l.fp, _ = os.Create(l.c.Filename)
						l.fileLogger.SetOutput(l.fp)
					}
				}
			}
		}
		_ = l.fileLogger.Output(calldepth, levelToString[level]+msg)
	}
}

var global Logger

func Debugf(format string, v ...interface{}) {
	global.Outputf(LevelDebug, 3, format, v...)
}

func Infof(format string, v ...interface{}) {
	global.Outputf(LevelInfo, 3, format, v...)
}

func Warnf(format string, v ...interface{}) {
	global.Outputf(LevelWarn, 3, format, v...)
}

func Errorf(format string, v ...interface{}) {
	global.Outputf(LevelError, 3, format, v...)
}

func Debug(v ...interface{}) {
	global.Output(LevelDebug, 3, v...)
}

func Info(v ...interface{}) {
	global.Output(LevelInfo, 3, v...)
}

func Warn(v ...interface{}) {
	global.Output(LevelWarn, 3, v...)
}

func Error(v ...interface{}) {
	global.Output(LevelError, 3, v...)
}

func FatalIfErrorNotNil(err error) {
	if err != nil {
		global.Outputf(LevelError, 3, "fatal since error not nil. err=%+v", err)
		os.Exit(1)
	}
}

func Outputf(level Level, calldepth int, format string, v ...interface{}) {
	global.Outputf(level, calldepth, format, v...)
}

func Output(level Level, calldepth int, v ...interface{}) {
	global.Output(level, calldepth, v...)
}

// 这里不加锁保护，如果要调用Init函数初始化全局的Logger，那么由调用方保证调用Init函数时不会并发调用全局Logger的其他方法
func Init(c Config) error {
	var err error
	global, err = New(c)
	return err
}

func shortFileSuffix(calldepth int) string {
	_, file, line, ok := runtime.Caller(calldepth)
	if !ok {
		file = "???"
		line = 0
	}
	short := file
	for i := len(file) - 1; i > 0; i-- {
		if file[i] == '/' {
			short = file[i+1:]
			break
		}
	}
	file = short
	return fmt.Sprintf("  - %s:%d", file, line)
}

func init() {
	global, _ = New(Config{
		Level:      LevelDebug,
		IsToStdout: true,
	})
}
