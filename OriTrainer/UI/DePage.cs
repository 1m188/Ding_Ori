using System;
using System.Drawing;
using System.Windows.Forms;
using OriTrainer.De;

namespace OriTrainer.UI
{
    /// <summary>
    /// 终极版页：把内嵌的 FLiNG 终极版修改器释放、启动并 SetParent 嵌入本页容器。
    /// 嵌入失败自动降级为独立窗口。
    /// </summary>
    internal sealed class DePage : UserControl
    {
        private static readonly Color Bg = Color.FromArgb(24, 24, 28);
        private static readonly Color Fg = Color.FromArgb(225, 225, 230);
        private static readonly Color Dim = Color.FromArgb(150, 150, 158);

        private readonly Panel _hostPanel = new Panel();
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

            _hostPanel.Dock = DockStyle.Fill;
            _hostPanel.BackColor = Color.Black;
            _hostPanel.Resize += (s, e) => _host.ResizeTo(_hostPanel.ClientRectangle);

            var bottom = new Panel { Dock = DockStyle.Bottom, Height = 56, BackColor = Bg };
            _btn.Text = "启动并嵌入终极版修改器";
            _btn.AutoSize = true;
            _btn.Location = new Point(10, 14);
            _btn.BackColor = Color.FromArgb(45, 45, 52);
            _btn.ForeColor = Fg;
            _btn.FlatStyle = FlatStyle.Flat;
            _btn.Click += OnButtonClick;

            _status.AutoSize = false;
            _status.Location = new Point(10, 0);
            _status.Size = new Size(720, 12);
            _status.ForeColor = Dim;
            _status.Text = "";
            _status.TextAlign = ContentAlignment.MiddleLeft;

            bottom.Controls.Add(_btn);
            bottom.Controls.Add(_status);

            Controls.Add(_hostPanel);
            Controls.Add(bottom);
        }

        private void OnButtonClick(object sender, EventArgs e)
        {
            if (_running)
            {
                Cleanup();
                _status.ForeColor = Dim;
                _status.Text = "终极版修改器已关闭。";
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
                if (_host.TryEmbed(_hostPanel))
                {
                    _status.ForeColor = Color.FromArgb(90, 200, 110);
                    _status.Text = "已嵌入终极版修改器（FLiNG）。它的热键由它自己处理，与本页原版热键互不冲突。";
                }
                else
                {
                    _status.ForeColor = Color.FromArgb(255, 150, 40);
                    _status.Text = "未能捕获窗口，已降级为独立窗口模式（功能不受影响）。";
                }
                _running = true;
                _btn.Text = "关闭终极版修改器";
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
            if (!_running) { _host.Cleanup(); return; }
            _host.Cleanup();
            _running = false;
            _btn.Text = "启动并嵌入终极版修改器";
        }
    }
}
