using System;
using System.Drawing;
using System.Threading;
using System.Windows.Forms;
using OriTrainer.Core;
using OriTrainer.Ori;

namespace OriTrainer.UI
{
    /// <summary>主窗体：双 tab（原版自研功能 / 终极版内嵌 FLiNG），深色风灵月影风格。</summary>
    internal sealed class MainForm : Form
    {
        private static readonly Color Bg = Color.FromArgb(24, 24, 28);
        private static readonly Color BgPanel = Color.FromArgb(34, 34, 40);
        private static readonly Color Fg = Color.FromArgb(225, 225, 230);
        private static readonly Color Accent = Color.FromArgb(255, 150, 40);
        private static readonly Color Ok = Color.FromArgb(90, 200, 110);
        private static readonly Color Bad = Color.FromArgb(235, 95, 95);

        private readonly TabControl _tabs = new TabControl();
        private readonly OriPage _oriPage = new OriPage();
        private readonly DePage _dePage = new DePage();
        private readonly Label _statusLeft = new Label();
        private readonly Label _statusRight = new Label();
        private readonly CheckBox _topMost = new CheckBox();

        private readonly MemoryReader _mem = new MemoryReader();
        private readonly HotkeyPoller _hotkeys = new HotkeyPoller();
        private readonly CheatFeature[] _features = OriFeatures.BuildAll();
        private FeatureEngine _engine;
        private Thread _attachThread;
        private volatile bool _shutdown;

        public MainForm()
        {
            BuildUi();
            _oriPage.Bind(_features, OnFeatureToggled);

            _engine = new FeatureEngine(_features) { Mem = _mem };
            _engine.StatusChanged += OnEngineStatus;

            _hotkeys.NumberPressed += OnHotkeyNumber;
            _hotkeys.HomePressed += OnHotkeyHome;

            StartAttachWatcher();
            _hotkeys.Start();
            _engine.Start();
        }

        // ---------- UI 构建 ----------

        private void BuildUi()
        {
            Text = "OriTrainer v0.1 — 奥日与迷失森林（原版 + 终极版）";
            Font = new Font("Microsoft YaHei UI", 9F);
            BackColor = Bg;
            ForeColor = Fg;
            FormBorderStyle = FormBorderStyle.FixedSingle;
            MaximizeBox = false;
            StartPosition = FormStartPosition.CenterScreen;
            ClientSize = new Size(640, 500);
            Icon = null;

            var top = new Panel { Dock = DockStyle.Top, Height = 36, BackColor = Bg };
            var title = new Label
            {
                Text = "OriTrainer 奥日与迷失森林修改器",
                ForeColor = Accent,
                Font = new Font("Microsoft YaHei UI", 11F, FontStyle.Bold),
                AutoSize = true,
                Location = new Point(10, 7)
            };
            _topMost.Text = "窗口置顶";
            _topMost.Checked = true;
            _topMost.AutoSize = true;
            _topMost.ForeColor = Fg;
            _topMost.Location = new Point(540, 8);
            _topMost.CheckedChanged += (s, e) => { TopMost = _topMost.Checked; };
            top.Controls.Add(title);
            top.Controls.Add(_topMost);
            TopMost = true;

            _tabs.Dock = DockStyle.Fill;
            var tpOri = new TabPage("原版 (ori.exe)") { BackColor = Bg, ForeColor = Fg };
            _oriPage.Dock = DockStyle.Fill;
            tpOri.Controls.Add(_oriPage);
            var tpDe = new TabPage("终极版 (oriDE.exe)") { BackColor = Bg, ForeColor = Fg };
            _dePage.Dock = DockStyle.Fill;
            tpDe.Controls.Add(_dePage);
            _tabs.TabPages.Add(tpOri);
            _tabs.TabPages.Add(tpDe);

            var bottom = new Panel { Dock = DockStyle.Bottom, Height = 28, BackColor = BgPanel };
            _statusLeft.Dock = DockStyle.Fill;
            _statusLeft.ForeColor = Bad;
            _statusLeft.TextAlign = ContentAlignment.MiddleLeft;
            _statusLeft.Text = "正在检测游戏进程…";
            _statusLeft.Padding = new Padding(8, 0, 0, 0);
            _statusRight.Dock = DockStyle.Right;
            _statusRight.ForeColor = Fg;
            _statusRight.TextAlign = ContentAlignment.MiddleRight;
            _statusRight.Width = 330;
            _statusRight.Text = "待机";
            bottom.Controls.Add(_statusLeft);
            bottom.Controls.Add(_statusRight);

            Controls.Add(_tabs);
            Controls.Add(top);
            Controls.Add(bottom);
        }

        // ---------- 进程附加 ----------

        private void StartAttachWatcher()
        {
            _attachThread = new Thread(() =>
            {
                while (!_shutdown)
                {
                    bool ok;
                    try { ok = _mem.Attached; }
                    catch (Exception) { ok = false; }

                    if (!ok)
                    {
                        _mem.Detach();
                        ok = _mem.Attach(OriOffsets.TargetProcessName);
                    }
                    UpdateProcessStatus(ok);
                    Thread.Sleep(2000);
                }
            })
            { IsBackground = true, Name = "AttachWatcher" };
            _attachThread.Start();
        }

        private void UpdateProcessStatus(bool attached)
        {
            if (IsDisposed || Disposing) return;
            try
            {
                BeginInvoke((Action)(() =>
                {
                    if (attached)
                    {
                        _statusLeft.Text = string.Format("已附加 ori.exe（PID {0}）—— 启动游戏后进入存档即可使用功能", _mem.ProcessId);
                        _statusLeft.ForeColor = Ok;
                    }
                    else
                    {
                        _statusLeft.Text = "未检测到原版游戏进程 ori.exe —— 请先启动游戏";
                        _statusLeft.ForeColor = Bad;
                    }
                }));
            }
            catch (InvalidOperationException) { }
        }

        private void OnEngineStatus(string s)
        {
            if (IsDisposed || Disposing) return;
            try
            {
                BeginInvoke((Action)(() => { _statusRight.Text = s; }));
            }
            catch (InvalidOperationException) { }
        }

        // ---------- 热键与功能开关 ----------

        private void OnHotkeyNumber(int idx)
        {
            int number = idx == 9 ? 0 : idx + 1;
            for (int i = 0; i < _features.Length; i++)
            {
                if (_features[i].HotkeyNumber == number)
                {
                    ToggleFeature(i, !_features[i].Active);
                    return;
                }
            }
        }

        private void OnHotkeyHome()
        {
            for (int i = 0; i < _features.Length; i++)
                if (_features[i].Active)
                    ToggleFeature(i, false);
        }

        private void OnFeatureToggled(int idx, bool on)
        {
            ToggleFeature(idx, on);
        }

        private void ToggleFeature(int idx, bool on)
        {
            if (idx < 0 || idx >= _features.Length) return;
            var f = _features[idx];
            if (f.Active != on)
                _engine.SetActive(f, on);
            try
            {
                // 热键轮询线程也会走到这里，勾选框更新必须调度回 UI 线程
                if (_oriPage.InvokeRequired)
                    BeginInvoke((Action)(() => _oriPage.SetItemChecked(idx, on)));
                else
                    _oriPage.SetItemChecked(idx, on);
            }
            catch (InvalidOperationException) { }
        }

        // ---------- 退出清理 ----------

        protected override void OnFormClosing(FormClosingEventArgs e)
        {
            _shutdown = true;
            try { _engine.DeactivateAll(); } catch (Exception) { }
            _engine.Stop();
            _hotkeys.Dispose();
            _dePage.Cleanup();
            _mem.Detach();
            base.OnFormClosing(e);
        }
    }
}
