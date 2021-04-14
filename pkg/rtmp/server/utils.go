package server

type clientConnectInfo struct {
	app            string
	flashVer       string
	swfUrl         string
	tcUrl          string
	fpad           bool
	audioCodecs    int
	videoCodecs    int
	videoFunction  int
	pageUrl        string
	objectEncoding int
}

type clientPublishOrPlayInfo struct {
	clientType uint8 //value: 0, 1(publish), 2(play)
	stream     string
	app        string
}

type onMetaData struct {
	Duration float64
	Filesize float64
	Encoder  string

	Width         float64
	Height        float64
	VideoCodecID  int
	Framerate     float64
	VideodataRate float64

	AudioCodecID    int
	Audiochannels   int
	Stereo          bool
	Audiodatarate   float64
	Audiosamplerate float64
	Audiosamplesize float64
}

func genStreamKey(vhost, appName, streamName string) string {
	return vhost + "/" + appName + "/" + streamName
}

func parseVhost(tcUrl string) (string, error) {
	//TODO:
	vhost := "127.0.0.1"
	return vhost, nil
}
