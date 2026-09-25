/*
    界面绘制：读 Status 里的当前状态，渲染成一屏文本交给 Terminal.Render 输出。

    本文件没有任何状态、也不接收任何参数 —— 每次调用都是把"此刻的全局状态"画一遍。
    所以渲染结果完全由 Status 决定，没有需要同步的第二份数据。

    ---- 关于对齐 ----
    参与对齐的是热键标签，它含中文（"小键盘"），所以按字符数补齐是错的：
    "小键盘 1" 是 5 字符 / 8 格，"Ctrl + 小键盘 1" 是 12 字符 / 15 格 ——
    字符数差 7、显示格数也差 7，看似一致，但那只是因为两个标签含**同样**的 3 个汉字。
    一旦加入不含"小键盘"的热键（如 HOME），按字符补齐就会错位。
    所以 Pad 按显示宽度（东亚字符算 2 格）补齐，而不是 string.Length。
    功能名是中文、长短不一，但排在行尾，不参与对齐，无需处理。

    ---- 为什么没有光标选中项 ----
    功能全部由全局热键（小键盘 / Ctrl+小键盘 / HOME）开关，Keyboard 也只上报这几类键，
    没有上下键可用，所以不存在"当前选中项"。界面是纯状态显示。
*/

using System.Text;

namespace OriTrainerDE
{
    internal static class UI
    {
        private const int Width = 58;      // 分隔线长度
        private const int HotkeyCells = 15; // 热键标签的显示宽度（"Ctrl + 小键盘 9" = 12 字符 / 15 格）

        public static string Build()
        {
            StringBuilder b = new StringBuilder();

            b.Append(Terminal.Title).Append("  奥日与迷失森林 · 终极版: 修改器")
             .Append(Terminal.Reset).Append('\n');
            Separator(b);

            StatusLine(b);
            Separator(b);

            // 每行自带完整键位，所以不需要再分组或加小标题
            foreach (Feature f in Status.Features)
                FeatureLine(b, f);

            Separator(b);

            b.Append("  ").Append(Terminal.Text).Append("小键盘").Append(Terminal.Reset)
             .Append(Terminal.Dim).Append(" 开关功能").Append(Terminal.Reset)
             .Append(Terminal.Dim).Append("   ·   ").Append(Terminal.Reset)
             .Append(Terminal.Text).Append("HOME").Append(Terminal.Reset)
             .Append(Terminal.Dim).Append(" 全部开 / 全部关").Append(Terminal.Reset).Append('\n');

            return b.ToString();
        }

        // 一行一个功能：热键（定宽对齐）→ 复选框 → 功能名。
        private static void FeatureLine(StringBuilder b, Feature f)
        {
            b.Append("  ");

            // 开启的行整行提亮、关闭的行整体压暗，扫一眼就能看出开了哪些
            if (f.On)
            {
                b.Append(Terminal.Text).Append(Pad(f.HotkeyLabel)).Append(Terminal.Reset)
                 .Append(Terminal.Green).Append("[x] ").Append(Terminal.Reset)
                 .Append(Terminal.Text).Append(f.Label).Append(Terminal.Reset);
            }
            else
            {
                b.Append(Terminal.Dim).Append(Pad(f.HotkeyLabel))
                 .Append("[ ] ").Append(f.Label).Append(Terminal.Reset);
            }

            b.Append('\n');
        }

        private static void StatusLine(StringBuilder b)
        {
            b.Append("  ");

            if (Status.Connection == ConnectionState.NoGame)
            {
                b.Append(Terminal.Red).Append("○ 未检测到游戏进程").Append(Terminal.Reset);
            }
            else
            {
                switch (Status.Connection)
                {
                    case ConnectionState.Connected:
                        b.Append(Terminal.Green).Append("● 已连接 (PID ").Append(Status.Pid).Append(')');
                        break;
                    case ConnectionState.Injecting:
                        b.Append(Terminal.Yellow).Append("◌ 正在注入… (PID ").Append(Status.Pid).Append(')');
                        break;
                    default:
                        b.Append(Terminal.Yellow).Append("○ 未连接 (PID ").Append(Status.Pid).Append(')');
                        break;
                }
                b.Append(Terminal.Reset);
            }

            b.Append('\n');
        }

        // 补齐到最长的热键标签宽度，让复选框与功能名左对齐。
        // 按**显示宽度**补（一个汉字占 2 格），不能按 string.Length：
        // 标签里有"小键盘"这 3 个汉字，字符数和显示格数不是一回事。
        private static string Pad(string s)
        {
            return s + new string(' ', HotkeyCells - Cells(s)) + "  ";
        }

        // 字符串的显示宽度：东亚宽字符算 2 格，其余算 1 格。
        // 只用于热键标签的对齐，不追求覆盖全部 Unicode 区间。
        private static int Cells(string s)
        {
            int n = 0;
            foreach (char c in s)
                n += (c >= 0x1100 && c <= 0x115F) || // 谚文字母
                     (c >= 0x2E80 && c <= 0xA4CF) || // 中日韩部首/汉字/假名等
                     (c >= 0xAC00 && c <= 0xD7A3) || // 谚文音节
                     (c >= 0xF900 && c <= 0xFAFF) || // 兼容汉字
                     (c >= 0xFE30 && c <= 0xFE6F) || // 中日韩兼容形式
                     (c >= 0xFF00 && c <= 0xFF60) || // 全角字符
                     (c >= 0xFFE0 && c <= 0xFFE6)
                     ? 2 : 1;
            return n;
        }

        private static void Separator(StringBuilder b)
        {
            b.Append(Terminal.Box).Append("  ").Append('─', Width).Append(Terminal.Reset).Append('\n');
        }
    }
}
