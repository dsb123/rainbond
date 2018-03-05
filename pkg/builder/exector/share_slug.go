// RAINBOND, Application Management Platform
// Copyright (C) 2014-2017 Goodrain Co., Ltd.

// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version. For any non-GPL usage of Rainbond,
// one or multiple Commercial Licenses authorized by Goodrain Co., Ltd.
// must be obtained first.

// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.

// You should have received a copy of the GNU General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package exector

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/coreos/etcd/clientv3"
	"github.com/goodrain/rainbond/pkg/builder/sources"
	"github.com/goodrain/rainbond/pkg/event"
	"github.com/pquerna/ffjson/ffjson"
)

//SlugShareItem SlugShareItem
type SlugShareItem struct {
	Namespace     string `json:"namespace"`
	TenantName    string `json:"tenant_name"`
	ServiceID     string `json:"service_id"`
	ServiceAlias  string `json:"service_alias"`
	SlugPath      string `json:"slug_path"`
	LocalSlugPath string `json:"local_slug_path"`
	ShareID       string `json:"share_id"`
	Logger        event.Logger
	ShareInfo     struct {
		ServiceKey string `json:"service_key" `
		AppVersion string `json:"app_version" `
		EventID    string `json:"event_id"`
		ShareUser  string `json:"share_user"`
		ShareScope string `json:"share_scope"`
		SlugInfo   struct {
			Namespace   string `json:"namespace"`
			FTPHost     string `json:"ftp_host"`
			FTPPort     string `json:"ftp_port"`
			FTPUser     string `json:"ftp_user"`
			FTPPassword string `json:"ftp_password"`
		} `json:"slug_info,omitempty"`
	} `json:"share_info"`
	EtcdCli     *clientv3.Client
	PackageName string
}

//NewSlugShareItem 创建实体
func NewSlugShareItem(in []byte, etcdCli *clientv3.Client) (*SlugShareItem, error) {
	var ssi SlugShareItem
	if err := ffjson.Unmarshal(in, &ssi); err != nil {
		return nil, err
	}
	eventID := ssi.ShareInfo.EventID
	ssi.Logger = event.GetManager().GetLogger(eventID)
	ssi.EtcdCli = etcdCli
	return &ssi, nil
}

//Run Run
func (i *SlugShareItem) ShareService() error {

	logrus.Debugf("分享应用，数据中心文件路径: %s ，分享目标路径 %s", i.LocalSlugPath, i.SlugPath)
	if _, err := os.Stat(i.LocalSlugPath); err != nil {
		i.Logger.Error(fmt.Sprintf("数据中心应用代码包不存在，请先构建应用"), map[string]string{"step": "slug-share", "status": "failure"})
		return err
	}
	if i.ShareInfo.SlugInfo.FTPHost != "" && i.ShareInfo.SlugInfo.FTPPort != "" {
		//share YS
		if err := i.ShareToFTP(); err != nil {
			return err
		}
	} else {
		if err := i.ShareToLocal(); err != nil {
			return err
		}
	}
	return nil
}

func createMD5(packageName string) (string, error) {
	md5Path := packageName + ".md5"
	_, err := os.Stat(md5Path)
	if err == nil {
		//md5 file exist
		return md5Path, nil
	}
	f, err := exec.Command("md5sum", packageName).Output()
	if err != nil {
		return "", err
	}
	md5In := strings.Split(string(f), "")
	logrus.Debugf("md5 value is %s", string(f))
	if err := ioutil.WriteFile(md5Path, []byte(md5In[0]), 0644); err != nil {
		return "", err
	}
	return md5Path, nil
}

//ShareToFTP ShareToFTP
func (i *SlugShareItem) ShareToFTP() error {
	file := i.LocalSlugPath
	i.Logger.Info("开始分享云帮", map[string]string{"step": "slug-share"})
	md5, err := createMD5(file)
	if err != nil {
		i.Logger.Error("生成md5失败", map[string]string{"step": "slug-share", "status": "failure"})
	}
	_ = md5
	//TODO:
	return nil
}

//ShareToLocal ShareToLocal
func (i *SlugShareItem) ShareToLocal() error {
	file := i.LocalSlugPath
	i.Logger.Info("开始分享应用到本地目录", map[string]string{"step": "slug-share"})
	md5, err := createMD5(file)
	if err != nil {
		i.Logger.Error("生成md5失败", map[string]string{"step": "slug-share", "status": "success"})
	}
	if err := sources.CopyFileWithProgress(i.LocalSlugPath, i.SlugPath, i.Logger); err != nil {
		os.Remove(i.SlugPath)
		logrus.Errorf("copy file to share path error: %s", err.Error())
		i.Logger.Error("复制文件失败", map[string]string{"step": "slug-share", "status": "failure"})
		return err
	}
	if err := sources.CopyFileWithProgress(md5, i.SlugPath+".md5", i.Logger); err != nil {
		os.Remove(i.SlugPath)
		os.Remove(i.SlugPath + ".md5")
		logrus.Errorf("copy file to share path error: %s", err.Error())
		i.Logger.Error("复制md5文件失败", map[string]string{"step": "slug-share", "status": "failure"})
		return err
	}
	i.Logger.Info("分享数据中心本地完成", map[string]string{"step": "slug-share", "status": "success"})
	return nil
}

//UploadFtp UploadFt
func (i *SlugShareItem) UploadFtp(path, file, md5 string) error {
	i.Logger.Info(fmt.Sprintf("开始上传代码包: %s", file), map[string]string{"step": "slug-share"})
	ftp := sources.NewFTPConnManager(i.Logger, i.ShareInfo.SlugInfo.FTPUser, i.ShareInfo.SlugInfo.FTPPassword, i.ShareInfo.SlugInfo.FTPHost, i.ShareInfo.SlugInfo.FTPPort)
	if err := ftp.FTPLogin(i.Logger); err != nil {
		return err
	}
	defer ftp.FTP.Close()
	curPath, err := ftp.FTPCWD(i.Logger, path)
	if err != nil {
		return err
	}
	if err := ftp.FTPUpload(i.Logger, curPath, file, md5); err != nil {
		return err
	}
	i.Logger.Info("代码包上传完成", map[string]string{"step": "slug-share", "status": "success"})
	return nil
}

//UpdateShareStatus 更新任务执行结果
func (i *SlugShareItem) UpdateShareStatus(status string) error {
	var ss = ShareStatus{
		ShareID: i.ShareID,
		Status:  status,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := i.EtcdCli.Put(ctx, fmt.Sprintf("/rainbond/shareresult/%s", i.ShareID), ss.String())
	if err != nil {
		logrus.Errorf("put shareresult  %s into etcd error, %v", i.ShareID, err)
		i.Logger.Error("存储检测结果失败。", map[string]string{"step": "callback", "status": "failure"})
	}
	i.Logger.Info("创建检测结果成功。", map[string]string{"step": "latest", "status": "success"})
	return nil
}

//CheckMD5FileExist CheckMD5FileExist
func (i *SlugShareItem) CheckMD5FileExist(md5path, packageName string) bool {
	return false
}
