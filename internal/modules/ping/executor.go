package ping

func Execute(req Request) Response {
	data := req.Data
	if data == "" {
		data = "pong"
	}

	if data == "crash" {
		return Response{
			Failed: true,
			Msg:    "boom",
		}
	}

	return Response{
		Changed: false,
		Ping:    data,
	}
}
