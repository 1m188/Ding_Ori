using System;
using System.Drawing;
using System.Windows.Forms;
using OriTrainer.Core;

namespace OriTrainer.UI
{
    /// <summary>原版功能页：功能开关列表 + 热键说明。</summary>
    internal sealed class OriPage : UserControl
    {
        private static readonly Color Bg = Color.FromArgb(24, 24, 28);
        private static readonly Color Fg = Color.FromArgb(225, 225, 230);
        private static readonly Color Dim = Color.FromArgb(150, 150, 158);
        private static readonly Color Accent = Color.FromArgb(255, 150, 40);

        private readonly CheckedListBox _list = new CheckedListBox();
        private Action<int, bool> _toggled;
        private int _sync;

        public OriPage()
        {
            BuildUi();
        }

        private void BuildUi()
        {
            BackColor = Bg;

            var header = new Label
            {
                Dock = DockStyle.Top,
                Height = 52,
                ForeColor = Dim,
                Text = "  先启动原版游戏（ori.exe），本页自动附加。数字键 1-9/0 切换对应功能（大键盘/小键盘均可），\r\n  HOME 关闭全部功能。勾选状态与热键实时同步。",
                Padding = new Padding(4, 4, 0, 0)
            };

            var footer = new Label
            {
                Dock = DockStyle.Bottom,
                Height = 44,
                ForeColor = Dim,
                Text = "  地址来源：FearLessRevolution 社区 CE 表（ubiByte / Dix Dark），详见 Ori/OriOffsets.cs。\r\n  标注 [待验证] 的功能来自社区表转写，尚未实机确认；无效请在游戏内反馈修正。",
                Padding = new Padding(4, 2, 0, 0)
            };

            _list.Dock = DockStyle.Fill;
            _list.CheckOnClick = true;
            _list.BackColor = Color.FromArgb(28, 28, 33);
            _list.ForeColor = Fg;
            _list.BorderStyle = BorderStyle.None;
            _list.Font = new Font("Microsoft YaHei UI", 10.5F);
            _list.ItemCheck += OnItemCheck;

            Controls.Add(_list);
            Controls.Add(footer);
            Controls.Add(header);
        }

        private void OnItemCheck(object sender, ItemCheckEventArgs e)
        {
            if (_sync > 0) return;
            var h = _toggled;
            if (h != null) h(e.Index, e.NewValue == CheckState.Checked);
        }

        public void Bind(CheatFeature[] features, Action<int, bool> toggled)
        {
            _toggled = toggled;
            _list.BeginUpdate();
            _list.Items.Clear();
            foreach (var f in features)
            {
                string tag = f.Verified ? "" : "   [待验证]";
                _list.Items.Add(string.Format("数字键 {0}    {1}{2}", f.HotkeyNumber, f.Name, tag));
            }
            _list.EndUpdate();
        }

        /// <summary>热键轮询线程同步勾选状态（不回触发事件）。</summary>
        public void SetItemChecked(int idx, bool on)
        {
            if (idx < 0 || idx >= _list.Items.Count) return;
            if (_list.GetItemChecked(idx) == on) return;
            _sync++;
            try { _list.SetItemChecked(idx, on); }
            finally { _sync--; }
        }
    }
}
