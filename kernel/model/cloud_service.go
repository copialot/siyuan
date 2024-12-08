// SiYuan - Refactor your thinking
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package model

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/88250/gulu"
	"github.com/88250/lute/parse"
	"github.com/siyuan-note/httpclient"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/task"
	"github.com/siyuan-note/siyuan/kernel/util"
)

var ErrFailedToConnectCloudServer = errors.New("failed to connect cloud server")

func CloudChatGPT(msg string, contextMsgs []string) (ret string, stop bool, err error) {
	if nil == Conf.GetUser() {
		return
	}

	payload := map[string]interface{}{}
	var messages []map[string]interface{}
	for _, contextMsg := range contextMsgs {
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": contextMsg,
		})
	}
	messages = append(messages, map[string]interface{}{
		"role":    "user",
		"content": msg,
	})
	payload["messages"] = messages

	requestResult := gulu.Ret.NewResult()
	request := httpclient.NewCloudRequest30s()
	_, err = request.
		SetSuccessResult(requestResult).
		SetCookies(&http.Cookie{Name: "symphony", Value: Conf.GetUser().UserToken}).
		SetBody(payload).
		Post(util.GetCloudServer() + "/apis/siyuan/ai/chatGPT")
	if err != nil {
		logging.LogErrorf("chat gpt failed: %s", err)
		err = ErrFailedToConnectCloudServer
		return
	}
	if 0 != requestResult.Code {
		err = errors.New(requestResult.Msg)
		stop = true
		return
	}

	data := requestResult.Data.(map[string]interface{})
	choices := data["choices"].([]interface{})
	if 1 > len(choices) {
		stop = true
		return
	}
	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})
	ret = message["content"].(string)

	if nil != choice["finish_reason"] {
		finishReason := choice["finish_reason"].(string)
		if "length" == finishReason {
			stop = false
		} else {
			stop = true
		}
	} else {
		stop = true
	}
	return
}

func StartFreeTrial() (err error) {
	if nil == Conf.GetUser() {
		return errors.New(Conf.Language(31))
	}

	requestResult := gulu.Ret.NewResult()
	request := httpclient.NewCloudRequest30s()
	resp, err := request.
		SetSuccessResult(requestResult).
		SetCookies(&http.Cookie{Name: "symphony", Value: Conf.GetUser().UserToken}).
		Post(util.GetCloudServer() + "/apis/siyuan/user/startFreeTrial")
	if err != nil {
		logging.LogErrorf("start free trial failed: %s", err)
		return ErrFailedToConnectCloudServer
	}
	if http.StatusOK != resp.StatusCode {
		logging.LogErrorf("start free trial failed: %d", resp.StatusCode)
		return ErrFailedToConnectCloudServer
	}
	if 0 != requestResult.Code {
		return errors.New(requestResult.Msg)
	}
	return
}

func DeactivateUser() (err error) {
	requestResult := gulu.Ret.NewResult()
	request := httpclient.NewCloudRequest30s()
	resp, err := request.
		SetSuccessResult(requestResult).
		SetCookies(&http.Cookie{Name: "symphony", Value: Conf.GetUser().UserToken}).
		Post(util.GetCloudServer() + "/apis/siyuan/user/deactivate")
	if err != nil {
		logging.LogErrorf("deactivate user failed: %s", err)
		return ErrFailedToConnectCloudServer
	}

	if 401 == resp.StatusCode {
		err = errors.New(Conf.Language(31))
		return
	}

	if 0 != requestResult.Code {
		logging.LogErrorf("deactivate user failed: %s", requestResult.Msg)
		return errors.New(requestResult.Msg)
	}
	return
}

func SetCloudBlockReminder(id, data string, timed int64) (err error) {
	requestResult := gulu.Ret.NewResult()
	payload := map[string]interface{}{"dataId": id, "data": data, "timed": timed}
	request := httpclient.NewCloudRequest30s()
	resp, err := request.
		SetSuccessResult(requestResult).
		SetBody(payload).
		SetCookies(&http.Cookie{Name: "symphony", Value: Conf.GetUser().UserToken}).
		Post(util.GetCloudServer() + "/apis/siyuan/calendar/setBlockReminder")
	if err != nil {
		logging.LogErrorf("set block reminder failed: %s", err)
		return ErrFailedToConnectCloudServer
	}

	if 401 == resp.StatusCode {
		err = errors.New(Conf.Language(31))
		return
	}

	if 0 != requestResult.Code {
		logging.LogErrorf("set block reminder failed: %s", requestResult.Msg)
		return errors.New(requestResult.Msg)
	}
	return
}

var uploadToken = ""
var uploadTokenTime int64

func LoadUploadToken() (err error) {
	now := time.Now().Unix()
	if 3600 >= now-uploadTokenTime {
		return
	}

	requestResult := gulu.Ret.NewResult()
	request := httpclient.NewCloudRequest30s()
	resp, err := request.
		SetSuccessResult(requestResult).
		SetCookies(&http.Cookie{Name: "symphony", Value: Conf.GetUser().UserToken}).
		Post(util.GetCloudServer() + "/apis/siyuan/upload/token")
	if err != nil {
		logging.LogErrorf("get upload token failed: %s", err)
		return ErrFailedToConnectCloudServer
	}

	if 401 == resp.StatusCode {
		err = errors.New(Conf.Language(31))
		return
	}

	if 0 != requestResult.Code {
		logging.LogErrorf("get upload token failed: %s", requestResult.Msg)
		return
	}

	resultData := requestResult.Data.(map[string]interface{})
	uploadToken = resultData["uploadToken"].(string)
	uploadTokenTime = now
	return
}

var (
	subscriptionExpirationReminded bool
)

func RefreshCheckJob() {
	go util.GetRhyResult(true) // 发一次请求进行结果缓存
	go refreshSubscriptionExpirationRemind()
	go refreshUser()
	go refreshAnnouncement()
	go refreshCheckDownloadInstallPkg()
}

func refreshSubscriptionExpirationRemind() {
	if subscriptionExpirationReminded {
		return
	}
	subscriptionExpirationReminded = true

	if "ios" == util.Container {
		return
	}

	defer logging.Recover()

	if IsSubscriber() && -1 != Conf.GetUser().UserSiYuanProExpireTime {
		expired := int64(Conf.GetUser().UserSiYuanProExpireTime)
		now := time.Now().UnixMilli()
		if now >= expired { // 已经过期
			if now-expired <= 1000*60*60*24*2 { // 2 天内提醒 https://github.com/siyuan-note/siyuan/issues/7816
				task.AppendAsyncTaskWithDelay(task.PushMsg, 30*time.Second, util.PushErrMsg, Conf.Language(128), 0)
			}
			return
		}
		remains := int((expired - now) / 1000 / 60 / 60 / 24)
		expireDay := 15 // 付费订阅提前 15 天提醒
		if 2 == Conf.GetUser().UserSiYuanSubscriptionPlan {
			expireDay = 3 // 试用订阅提前 3 天提醒
		}

		if 0 < remains && expireDay > remains {
			task.AppendAsyncTaskWithDelay(task.PushMsg, 7*time.Second, util.PushErrMsg, fmt.Sprintf(Conf.Language(127), remains), 0)
			return
		}
	}
}

func refreshUser() {
	defer logging.Recover()

	if nil != Conf.GetUser() {
		time.Sleep(2 * time.Minute)
		if nil != Conf.GetUser() {
			RefreshUser(Conf.GetUser().UserToken)
		}
		subscriptionExpirationReminded = false
	}
}

func refreshCheckDownloadInstallPkg() {
	defer logging.Recover()

	time.Sleep(3 * time.Minute)
	checkDownloadInstallPkg()
	if "" != getNewVerInstallPkgPath() {
		util.PushMsg(Conf.Language(62), 15*1000)
	}
}

func refreshAnnouncement() {
	defer logging.Recover()

	time.Sleep(1 * time.Minute)
	announcementConf := filepath.Join(util.HomeDir, ".config", "note", "announcement.json")
	var existingAnnouncements, newAnnouncements []*Announcement
	if gulu.File.IsExist(announcementConf) {
		data, err := os.ReadFile(announcementConf)
		if err != nil {
			logging.LogErrorf("read announcement conf failed: %s", err)
			return
		}
		if err = gulu.JSON.UnmarshalJSON(data, &existingAnnouncements); err != nil {
			logging.LogErrorf("unmarshal announcement conf failed: %s", err)
			os.Remove(announcementConf)
			return
		}
	}

	for _, announcement := range GetAnnouncements() {
		var exist bool
		for _, existingAnnouncement := range existingAnnouncements {
			if announcement.Id == existingAnnouncement.Id {
				exist = true
				break
			}
		}
		if !exist {
			existingAnnouncements = append(existingAnnouncements, announcement)
			if Conf.CloudRegion == announcement.Region {
				newAnnouncements = append(newAnnouncements, announcement)
			}
		}
	}

	data, err := gulu.JSON.MarshalJSON(existingAnnouncements)
	if err != nil {
		logging.LogErrorf("marshal announcement conf failed: %s", err)
		return
	}
	if err = os.WriteFile(announcementConf, data, 0644); err != nil {
		logging.LogErrorf("write announcement conf failed: %s", err)
		return
	}

	for _, newAnnouncement := range newAnnouncements {
		util.PushMsg(fmt.Sprintf(Conf.Language(11), newAnnouncement.URL, newAnnouncement.Title), 0)
	}
}

func RefreshUser(token string) {
	threeDaysAfter := util.CurrentTimeMillis() + 1000*60*60*24*3
	Conf.SetUser(loadUserFromConf())
	if "" == token {
		if "" != Conf.UserData {
			Conf.SetUser(loadUserFromConf())
		}
		if nil == Conf.GetUser() {
			return
		}

		var tokenExpireTime int64
		tokenExpireTime, err := strconv.ParseInt(Conf.GetUser().UserTokenExpireTime+"000", 10, 64)
		if err != nil {
			logging.LogErrorf("convert token expire time [%s] failed: %s", Conf.GetUser().UserTokenExpireTime, err)
			util.PushErrMsg(Conf.Language(19), 5000)
			return
		}

		if threeDaysAfter > tokenExpireTime {
			token = Conf.GetUser().UserToken
			goto Net
		}
		return
	}

Net:
	start := time.Now()
	user, err := getUser(token)
	if err != nil {
		if nil == Conf.GetUser() || errInvalidUser == err {
			util.PushErrMsg(Conf.Language(19), 5000)
			return
		}

		var tokenExpireTime int64
		tokenExpireTime, err = strconv.ParseInt(Conf.GetUser().UserTokenExpireTime+"000", 10, 64)
		if err != nil {
			logging.LogErrorf("convert token expire time [%s] failed: %s", Conf.GetUser().UserTokenExpireTime, err)
			util.PushErrMsg(Conf.Language(19), 5000)
			return
		}

		if threeDaysAfter > tokenExpireTime {
			util.PushErrMsg(Conf.Language(19), 5000)
			return
		}
		return
	}

	Conf.SetUser(user)
	data, _ := gulu.JSON.MarshalJSON(user)
	Conf.UserData = util.AESEncrypt(string(data))
	Conf.Save()

	if elapsed := time.Now().Sub(start).Milliseconds(); 3000 < elapsed {
		logging.LogInfof("get cloud user elapsed [%dms]", elapsed)
	}
	return
}

func loadUserFromConf() *conf.User {
	userStr := `{"userId":"1719061610011","userName":"neo","userAvatarURL":"https://assets.b3logfile.com/avatar/1719061610038.png?imageView2/1/w/256/h/256/interlace/0/q/100","userHomeBImgURL":"","userIntro":"","userNickname":"","userCreateTime":"20240622 21:06:50","userSiYuanProExpireTime":-1,"userToken":"b1d0d1ad98acdd5d7d846d4359048a923f932962125e7ad0c9db814d5942879467b648f693d6a11336bcaa8b1bee42090ae1061a025b6f16d8afdc6baac0c1129f62ce384f54ec3360273c1f53642adebbfd8d37a5170806c144c59ee5044cc88dd6e440a06e8853cfdf59e8a45800ec8b5e8bd41105485f1487839127608f94ed7fe815944a37852dd7a501050b20406a145b7f233259ca64051fb209eb0c988e2c8d1cef2fa81ae3991e267f9b3a8ae73983e05a43575e4431f814449b3e2b990f0182ec8d7fad6e834efa68396f913fc52221695f3125e772d5f907eb382e1da1b902feab17c7d10bf64ecd567f81","userTokenExpireTime":"7275693060","userSiYuanRepoSize":0,"userSiYuanPointExchangeRepoSize":0,"userSiYuanAssetSize":0,"userTrafficUpload":0,"userTrafficDownload":0,"userTrafficAPIGet":0,"userTrafficAPIPut":0,"userTrafficTime":0,"userSiYuanSubscriptionPlan":0,"userSiYuanSubscriptionStatus":0,"userSiYuanSubscriptionType":1,"userSiYuanOneTimePayStatus":1}`
	user := &conf.User{}
	gulu.JSON.UnmarshalJSON([]byte(userStr), &user)

	fmt.Print("inject user success")
	return user
	// return user1
	// if "" == Conf.UserData {
	// 	return nil
	// }
	// data := util.AESDecrypt(Conf.UserData)
	// data, _ = hex.DecodeString(string(data))
	// user := &conf.User{}
	// if err := gulu.JSON.UnmarshalJSON(data, &user); nil == err {
	// 	return user
	// }

	// return nil
}

func RemoveCloudShorthands(ids []string) (err error) {
	result := map[string]interface{}{}
	request := httpclient.NewCloudRequest30s()
	body := map[string]interface{}{
		"ids": ids,
	}
	resp, err := request.
		SetSuccessResult(&result).
		SetCookies(&http.Cookie{Name: "symphony", Value: Conf.GetUser().UserToken}).
		SetBody(body).
		Post(util.GetCloudServer() + "/apis/siyuan/inbox/removeCloudShorthands")
	if err != nil {
		logging.LogErrorf("remove cloud shorthands failed: %s", err)
		err = ErrFailedToConnectCloudServer
		return
	}

	if 401 == resp.StatusCode {
		err = errors.New(Conf.Language(31))
		return
	}

	code := result["code"].(float64)
	if 0 != code {
		logging.LogErrorf("remove cloud shorthands failed: %s", result["msg"])
		err = errors.New(result["msg"].(string))
		return
	}
	return
}

func GetCloudShorthand(id string) (ret map[string]interface{}, err error) {
	result := map[string]interface{}{}
	request := httpclient.NewCloudRequest30s()
	resp, err := request.
		SetSuccessResult(&result).
		SetCookies(&http.Cookie{Name: "symphony", Value: Conf.GetUser().UserToken}).
		Post(util.GetCloudServer() + "/apis/siyuan/inbox/getCloudShorthand?id=" + id)
	if err != nil {
		logging.LogErrorf("get cloud shorthand failed: %s", err)
		err = ErrFailedToConnectCloudServer
		return
	}

	if 401 == resp.StatusCode {
		err = errors.New(Conf.Language(31))
		return
	}

	code := result["code"].(float64)
	if 0 != code {
		logging.LogErrorf("get cloud shorthand failed: %s", result["msg"])
		err = errors.New(result["msg"].(string))
		return
	}
	ret = result["data"].(map[string]interface{})
	t, _ := strconv.ParseInt(id, 10, 64)
	hCreated := util.Millisecond2Time(t)
	ret["hCreated"] = hCreated.Format("2006-01-02 15:04")

	md := ret["shorthandContent"].(string)
	ret["shorthandMd"] = md

	luteEngine := NewLute()
	luteEngine.SetFootnotes(true)
	tree := parse.Parse("", []byte(md), luteEngine.ParseOptions)
	content := luteEngine.ProtylePreview(tree, luteEngine.RenderOptions)
	ret["shorthandContent"] = content
	return
}

func GetCloudShorthands(page int) (result map[string]interface{}, err error) {
	result = map[string]interface{}{}
	request := httpclient.NewCloudRequest30s()
	resp, err := request.
		SetSuccessResult(&result).
		SetCookies(&http.Cookie{Name: "symphony", Value: Conf.GetUser().UserToken}).
		Post(util.GetCloudServer() + "/apis/siyuan/inbox/getCloudShorthands?p=" + strconv.Itoa(page))
	if err != nil {
		logging.LogErrorf("get cloud shorthands failed: %s", err)
		err = ErrFailedToConnectCloudServer
		return
	}

	if 401 == resp.StatusCode {
		err = errors.New(Conf.Language(31))
		return
	}

	code := result["code"].(float64)
	if 0 != code {
		logging.LogErrorf("get cloud shorthands failed: %s", result["msg"])
		err = errors.New(result["msg"].(string))
		return
	}

	luteEngine := NewLute()
	audioRegexp := regexp.MustCompile("<audio.*>.*</audio>")
	videoRegexp := regexp.MustCompile("<video.*>.*</video>")
	fileRegexp := regexp.MustCompile("\\[文件]\\(.*\\)")
	shorthands := result["data"].(map[string]interface{})["shorthands"].([]interface{})
	for _, item := range shorthands {
		shorthand := item.(map[string]interface{})
		id := shorthand["oId"].(string)
		t, _ := strconv.ParseInt(id, 10, 64)
		hCreated := util.Millisecond2Time(t)
		shorthand["hCreated"] = hCreated.Format("2006-01-02 15:04")

		desc := shorthand["shorthandDesc"].(string)
		desc = audioRegexp.ReplaceAllString(desc, " 语音 ")
		desc = videoRegexp.ReplaceAllString(desc, " 视频 ")
		desc = fileRegexp.ReplaceAllString(desc, " 文件 ")
		desc = strings.ReplaceAll(desc, "\n\n", "")
		desc = strings.TrimSpace(desc)
		shorthand["shorthandDesc"] = desc

		md := shorthand["shorthandContent"].(string)
		shorthand["shorthandMd"] = md
		tree := parse.Parse("", []byte(md), luteEngine.ParseOptions)
		content := luteEngine.ProtylePreview(tree, luteEngine.RenderOptions)
		shorthand["shorthandContent"] = content
	}
	return
}

var errInvalidUser = errors.New("invalid user")

func getUser(token string) (*conf.User, error) {
	result := map[string]interface{}{}
	request := httpclient.NewCloudRequest30s()
	resp, err := request.
		SetSuccessResult(&result).
		SetBody(map[string]string{"token": token}).
		Post(util.GetCloudServer() + "/apis/siyuan/user")
	if err != nil {
		logging.LogErrorf("get community user failed: %s", err)
		return nil, errors.New(Conf.Language(18))
	}
	if http.StatusOK != resp.StatusCode {
		logging.LogErrorf("get community user failed: %d", resp.StatusCode)
		return nil, errors.New(Conf.Language(18))
	}

	code := result["code"].(float64)
	if 0 != code {
		if 255 == code {
			return nil, errInvalidUser
		}
		logging.LogErrorf("get community user failed: %s", result["msg"])
		return nil, errors.New(Conf.Language(18))
	}

	dataStr := result["data"].(string)
	data := util.AESDecrypt(dataStr)
	user := &conf.User{}
	if err = gulu.JSON.UnmarshalJSON(data, &user); err != nil {
		logging.LogErrorf("get community user failed: %s", err)
		return nil, errors.New(Conf.Language(18))
	}
	return user, nil
}

func UseActivationcode(code string) (err error) {
	code = strings.TrimSpace(code)
	code = util.RemoveInvalid(code)
	requestResult := gulu.Ret.NewResult()
	request := httpclient.NewCloudRequest30s()
	resp, err := request.
		SetSuccessResult(requestResult).
		SetBody(map[string]string{"data": code}).
		SetCookies(&http.Cookie{Name: "symphony", Value: Conf.GetUser().UserToken}).
		Post(util.GetCloudServer() + "/apis/siyuan/useActivationcode")
	if err != nil {
		logging.LogErrorf("check activation code failed: %s", err)
		return ErrFailedToConnectCloudServer
	}
	if http.StatusOK != resp.StatusCode {
		logging.LogErrorf("check activation code failed: %d", resp.StatusCode)
		return ErrFailedToConnectCloudServer
	}
	if 0 != requestResult.Code {
		return errors.New(requestResult.Msg)
	}
	return
}

func CheckActivationcode(code string) (retCode int, msg string) {
	code = strings.TrimSpace(code)
	code = util.RemoveInvalid(code)
	retCode = 1
	requestResult := gulu.Ret.NewResult()
	request := httpclient.NewCloudRequest30s()
	resp, err := request.
		SetSuccessResult(requestResult).
		SetBody(map[string]string{"data": code}).
		SetCookies(&http.Cookie{Name: "symphony", Value: Conf.GetUser().UserToken}).
		Post(util.GetCloudServer() + "/apis/siyuan/checkActivationcode")
	if err != nil {
		logging.LogErrorf("check activation code failed: %s", err)
		msg = ErrFailedToConnectCloudServer.Error()
		return
	}
	if http.StatusOK != resp.StatusCode {
		logging.LogErrorf("check activation code failed: %d", resp.StatusCode)
		msg = ErrFailedToConnectCloudServer.Error()
		return
	}
	if 0 == requestResult.Code {
		retCode = 0
	}
	msg = requestResult.Msg
	return
}

func Login(userName, password, captcha string, cloudRegion int) (ret *gulu.Result) {
	Conf.CloudRegion = cloudRegion
	Conf.Save()
	util.CurrentCloudRegion = cloudRegion

	result := map[string]interface{}{}
	request := httpclient.NewCloudRequest30s()
	resp, err := request.
		SetSuccessResult(&result).
		SetBody(map[string]string{"userName": userName, "userPassword": password, "captcha": captcha}).
		Post(util.GetCloudServer() + "/apis/siyuan/login")
	if err != nil {
		logging.LogErrorf("login failed: %s", err)
		ret = gulu.Ret.NewResult()
		ret.Code = -1
		ret.Msg = Conf.Language(18) + ": " + err.Error()
		return
	}
	if http.StatusOK != resp.StatusCode {
		logging.LogErrorf("login failed: %d", resp.StatusCode)
		ret = gulu.Ret.NewResult()
		ret.Code = -1
		ret.Msg = Conf.Language(18)
		return
	}

	ret = &gulu.Result{
		Code: int(result["code"].(float64)),
		Msg:  result["msg"].(string),
		Data: map[string]interface{}{
			"userName":    result["userName"],
			"token":       result["token"],
			"needCaptcha": result["needCaptcha"],
		},
	}
	if -1 == ret.Code {
		ret.Code = 1
	}
	return
}

func Login2fa(token, code string) (map[string]interface{}, error) {
	result := map[string]interface{}{}
	request := httpclient.NewCloudRequest30s()
	_, err := request.
		SetSuccessResult(&result).
		SetBody(map[string]string{"twofactorAuthCode": code}).
		SetHeader("token", token).
		Post(util.GetCloudServer() + "/apis/siyuan/login/2fa")
	if err != nil {
		logging.LogErrorf("login 2fa failed: %s", err)
		return nil, errors.New(Conf.Language(18))
	}
	return result, nil
}

func LogoutUser() {
	Conf.UserData = ""
	Conf.SetUser(nil)
	Conf.Save()
}
