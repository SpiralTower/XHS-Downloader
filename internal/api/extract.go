package api

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

func extractWork(note map[string]any, sourceURL, expectedID string) (map[string]any, error) {
	if len(note) == 0 {
		return nil, ErrNoteDataNotFound
	}
	id := firstString(note["noteId"])
	if id == "" {
		return nil, errors.New("note id is missing")
	}
	if expectedID != "" && id != expectedID {
		return nil, ErrNoteDataNotFound
	}

	images := asSlice(note["imageList"])
	typeName := classifyWork(firstString(note["type"]), len(images))
	user, _ := asMap(note["user"])
	interact, _ := asMap(note["interactInfo"])

	result := map[string]any{
		"收藏数量":   fieldOr(interact, "collectedCount", "-1"),
		"评论数量":   fieldOr(interact, "commentCount", "-1"),
		"分享数量":   fieldOr(interact, "shareCount", "-1"),
		"点赞数量":   fieldOr(interact, "likedCount", "-1"),
		"作品标签":   extractTags(note["tagList"]),
		"作品ID":   id,
		"作品链接":   sourceURL,
		"作品标题":   firstString(note["title"]),
		"作品描述":   firstString(note["desc"]),
		"作品类型":   typeName,
		"发布时间":   formattedTimestamp(note["time"]),
		"最后更新时间": formattedTimestamp(note["lastUpdateTime"]),
		"时间戳":    unixSeconds(note["time"]),
		"作者昵称":   firstString(user["nickname"], user["nickName"], id),
		"作者ID":   firstString(user["userId"]),
	}
	authorID := stringValue(result["作者ID"])
	if authorID != "" {
		result["作者链接"] = "https://www.xiaohongshu.com/user/profile/" + authorID
	} else {
		result["作者链接"] = ""
	}

	switch typeName {
	case "视频":
		result["下载地址"] = videoURLs(note, "resolution")
		result["动图地址"] = []any{nil}
	case "图文", "图集":
		media, lives := imageURLs(images, "jpeg")
		result["下载地址"] = media
		result["动图地址"] = lives
	default:
		result["下载地址"] = []string{}
		result["动图地址"] = []any{}
	}
	return result, nil
}

func fieldOr(object map[string]any, key string, fallback any) any {
	if object == nil {
		return fallback
	}
	value, ok := object[key]
	if !ok || value == nil || stringValue(value) == "" {
		return fallback
	}
	return value
}

func classifyWork(kind string, imageCount int) string {
	switch kind {
	case "video":
		if imageCount > 1 {
			return "图集"
		}
		if imageCount == 1 {
			return "视频"
		}
	case "normal":
		if imageCount > 0 {
			return "图文"
		}
	}
	return "未知"
}

func extractTags(value any) string {
	items := asSlice(value)
	names := make([]string, 0, len(items))
	for _, item := range items {
		object, ok := asMap(item)
		if !ok {
			continue
		}
		if name := firstString(object["name"]); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, " ")
}

func formattedTimestamp(value any) string {
	milliseconds := int64(floatValue(value))
	if milliseconds <= 0 {
		return "未知"
	}
	return time.UnixMilli(milliseconds).In(time.Local).Format("2006-01-02_15:04:05")
}

func unixSeconds(value any) any {
	milliseconds := floatValue(value)
	if milliseconds <= 0 {
		return nil
	}
	return milliseconds / 1000
}

func imageURLs(images []any, format string) ([]string, []any) {
	media := make([]string, 0, len(images))
	lives := make([]any, 0, len(images))
	for _, item := range images {
		mediaURL := ""
		var liveURL any
		if image, ok := asMap(item); ok {
			raw := firstString(image["urlDefault"], image["url"])
			if token := imageToken(raw); token != "" {
				if format == "auto" {
					mediaURL = "https://sns-img-bd.xhscdn.com/" + token
				} else {
					mediaURL = "https://ci.xiaohongshu.com/" + token + "?imageView2/format/" + format
				}
			}
			liveURL = firstLiveURL(image)
		}
		media = append(media, mediaURL)
		lives = append(lives, liveURL)
	}
	return media, lives
}

var imageTokenPathPattern = regexp.MustCompile("(?i)^/[0-9]+/[0-9a-z]+/([^!]+)!")

func imageToken(raw string) string {
	raw = normalizedURL(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	matches := imageTokenPathPattern.FindStringSubmatch(parsed.Path)
	if len(matches) != 2 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(matches[1]), "/")
}

func firstLiveURL(image map[string]any) any {
	stream, _ := asMap(image["stream"])
	h264 := asSlice(stream["h264"])
	if len(h264) == 0 {
		return nil
	}
	item, ok := asMap(h264[0])
	if !ok {
		return nil
	}
	if value := firstString(item["masterUrl"]); value != "" {
		return normalizedURL(value)
	}
	return nil
}

type videoStream struct {
	url     string
	height  float64
	bitrate float64
	size    float64
}

func videoURLs(note map[string]any, preference string) []string {
	if key := firstString(valueAt(note, "video", "consumer", "originVideoKey")); key != "" {
		return []string{"https://sns-video-bd.xhscdn.com/" + normalizedURL(key)}
	}
	media, _ := asMap(valueAt(note, "video", "media"))
	stream, _ := asMap(media["stream"])
	items := append(asSlice(stream["h264"]), asSlice(stream["h265"])...)
	streams := make([]videoStream, 0, len(items))
	for _, item := range items {
		object, ok := asMap(item)
		if !ok {
			continue
		}
		streamURL := ""
		for _, backup := range asSlice(object["backupUrls"]) {
			if streamURL = firstString(backup); streamURL != "" {
				break
			}
		}
		if streamURL == "" {
			streamURL = firstString(object["masterUrl"])
		}
		if streamURL == "" {
			continue
		}
		streams = append(streams, videoStream{
			url:     normalizedURL(streamURL),
			height:  floatValue(object["height"]),
			bitrate: floatValue(object["videoBitrate"]),
			size:    floatValue(object["size"]),
		})
	}
	if len(streams) == 0 {
		return []string{}
	}
	sort.SliceStable(streams, func(i, j int) bool {
		switch preference {
		case "bitrate":
			return streams[i].bitrate < streams[j].bitrate
		case "size":
			return streams[i].size < streams[j].size
		default:
			return streams[i].height < streams[j].height
		}
	})
	return []string{streams[len(streams)-1].url}
}

func mediaURLs(data map[string]any) ([]string, []any, error) {
	urls, err := mediaStringList(data["下载地址"])
	if err != nil {
		return nil, nil, fmt.Errorf("invalid media URL list")
	}
	lives, err := mediaAnyList(data["动图地址"])
	if err != nil {
		return nil, nil, fmt.Errorf("invalid live media URL list")
	}
	return urls, lives, nil
}

func mediaStringList(value any) ([]string, error) {
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...), nil
	case []any:
		result := make([]string, len(items))
		for index, item := range items {
			text, ok := item.(string)
			if !ok {
				return nil, errors.New("media URL entries must be strings")
			}
			result[index] = text
		}
		return result, nil
	default:
		return nil, errors.New("media URLs must be an array")
	}
}

func mediaAnyList(value any) ([]any, error) {
	switch items := value.(type) {
	case nil:
		return nil, nil
	case []any:
		return append([]any(nil), items...), nil
	case []string:
		result := make([]any, len(items))
		for index, item := range items {
			result[index] = item
		}
		return result, nil
	default:
		return nil, errors.New("live media URLs must be an array")
	}
}
