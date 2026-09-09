using System;
using System.Drawing;
using System.Windows.Forms;
using OriTrainer.De;

namespace OriTrainer.UI
{
    /// <summary>
    /// 终极版页：一键启动/关闭内嵌的 FLiNG 终极版修改器（独立窗口，自动定位到本程序旁边）。
    /// 实测该修改器渲染引擎不支持作为子窗口嵌入，故采用旁挂方式，生命周期由本程序托管。
    /// </summary>
    internal sealed class DePage : UserControl
    {
        private static readonly Color Bg = Color.FromArgb(24, 24, 28);
        private static readonly Color Fg = Color.FromArgb(225, 225, 230);
        private static readonly Color Dim = Color.FromArgb(150, 150, 158);
        private static readonly Color Accent = Color.FromArgb(255, 150, 40);

        private readonly Button _btn = new Button();
        private readonly Label _status = new Label();
        private readonly DeTrainerHost _host = new DeTrainerHost();
        private bool _running;

        public DePage()
        {
            BuildUi();
        }

        private void BuildUi()
        {
            BackColor = Bg;

            var info = new Label
            {
                Dock = DockStyle.Fill,
                BackColor = Bg,
                ForeColor = Dim,
                TextAlign = ContentAlignment.MiddleCenter,
                Text = "终极版修改器（FLiNG 官方，已内嵌于本程序）\r\n\r\n"
                     + "点击下方按钮一键启动，窗口会自动出现在本程序旁边；\r\n"
                     + "退出本程序时会自动关闭它并清理临时文件。\r\n\r\n"
                     + "注：该修改器的渲染引擎不支持作为子窗口嵌入（实测），\r\n"
                     + "故采用旁挂独立窗口方式；其数字键热键与本程序互不冲突。\r\n\r\n"
                     + "版权归 FLiNG@3DMGAME 所有，内嵌仅限个人使用。",
                Font = new Font("Microsoft YaHei UI", 9.5F),
                Padding = new Padding(8)
            };

            var bottom = new Panel { Dock = DockStyle.Bottom, Height = 56, BackColor = Bg };
            _btn.Text = "启动终极版修改器";
            _btn.AutoSize = true;
            _btn.Location = new Point(10, 14);
            _btn.BackColor = Color.FromArgb(45, 45, 52);
            _btn.ForeColor = Fg;
            _btn.FlatStyle = FlatStyle.Flat;
            _btn.Click += OnButtonClick;

            _status.AutoSize = false;
            _status.Location = new Point(10, 0);
            _status.Size = new Size(620, 14);
            _status.ForeColor = Dim;
            _status.TextAlign = ContentAlignment.MiddleLeft;

            bottom.Controls.Add(_btn);
            bottom.Controls.Add(_status);

            Controls.Add(info);
            Controls.Add(bottom);
        }

        private void OnButtonClick(object sender, EventArgs e)
        {
            if (_running)
            {
                Cleanup();
                _status.ForeColor = Dim;
                _status.Text = "终极版修改器已关闭，临时文件已清理。";
                return;
            }

            try
            {
                _status.ForeColor = Dim;
                _status.Text = "正在释放并启动终极版修改器…";
                _host.Extract();
                if (!_host.Launch())
                {
                    _status.ForeColor = Color.FromArgb(235, 95, 95);
                    _status.Text = "启动失败（进程创建失败）。";
                    return;
                }

                _status.Text = "等待修改器窗口出现…";
                var form = FindForm();
                bool positioned = _host.WaitAndPositionNextTo(form != null ? form.Handle : IntPtr.Zero);
                if (_host.IsRunning)
                {
                    _status.ForeColor = Color.FromArgb(90, 200, 110);
                    _status.Text = positioned
                        ? "终极版修改器已启动（独立窗口，已定位到本程序旁边）。"
                        : "终极版修改器已启动（独立窗口）。";
                    _running = true;
                    _btn.Text = "关闭终极版修改器";
                }
                else
                {
                    _status.ForeColor = Color.FromArgb(235, 95, 95);
                    _status.Text = "修改器进程已退出（可能被杀软拦截），请加白名单后重试。";
                }
            }
            catch (Exception ex)
            {
                _status.ForeColor = Color.FromArgb(235, 95, 95);
                _status.Text = "出错: " + ex.Message;
            }
        }

        /// <summary>主窗体关闭时调用：结束子进程并清理临时文件。</summary>
        public void Cleanup()
        {
            _host.Cleanup();
            _running = false;
            _btn.Text = "启动终极版修改器";
        }
    }
}
