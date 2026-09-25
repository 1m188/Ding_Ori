/*
    键盘监听：后台线程全局轮询按键，主循环按需取走。

    只上报三类键，其余一律丢弃：
        小键盘 0-9        0x60..0x69
        Ctrl+小键盘 0-9   同上，Ctrl 状态作为事件的字段一并带上
        HOME              0x24
    过滤放在这里（而不是让主循环收下全部再筛）是因为轮询本身就要遍历键码：
    不关心的键连 GetAsyncKeyState 都不必调，几微秒就能扫完一遍。

    ⚠ 本文件只负责"报告哪个键被按下了"，不做任何功能判断、不改任何状态 ——
    哪个键对应哪个功能、开了还是关着，全在主循环里决定。

    ---- 为什么是队列而不是 C# 的 event ----
    event 的处理器在**触发它的线程**上执行。轮询跑在后台线程，用 event 会把主循环的
    处理逻辑拉到后台线程上跑，和主循环的渲染并发改写同一份功能表 —— 正是要避免的。
    队列把生产者（后台线程）和消费者（主循环）解耦：处理永远发生在主循环里，
    按键顺序也不会乱。

    ---- 为什么需要后台线程 ----
    GetAsyncKeyState 报的是"此刻是否按下"，是电平而非脉冲：主循环 80ms 一轮，
    一次短促的敲击完全可能落在两轮之间、被整个漏掉。后台线程 10ms 采一次，
    漏采窗口缩小 8 倍；事件进队列后，即使主循环正在注入 + 连管道（最长数秒）
    也不会丢按键。
*/

using System;
using System.Collections.Concurrent;
using System.Runtime.InteropServices;
using System.Threading;

namespace OriTrainerDE
{
    // 一次按下：虚拟键码 + 按下瞬间的 Ctrl 状态。
    internal struct KeyEvent
    {
        public int VirtualKey;
        public bool Ctrl;
    }

    internal static class Keyboard
    {
        private const int PollMs = 10;

        private const int VkHome = 0x24;
        private const int VkNumPad0 = 0x60; // 小键盘 0；1..9 依次为 0x61..0x69
        private const int VkControl = 0x11;

        // 唯一的上报白名单。要加键就在这里加一行 —— 除此之外没有别的地方需要改。
        private static readonly int[] Watched =
        {
            VkHome,
            VkNumPad0, VkNumPad0 + 1, VkNumPad0 + 2, VkNumPad0 + 3, VkNumPad0 + 4,
            VkNumPad0 + 5, VkNumPad0 + 6, VkNumPad0 + 7, VkNumPad0 + 8, VkNumPad0 + 9,
        };

        private static readonly ConcurrentQueue<KeyEvent> _queue = new ConcurrentQueue<KeyEvent>();

        // 下标即虚拟键码，记录上一轮的按下状态，用于把电平折成边沿。
        // 只有轮询线程读写，不需要同步。
        private static readonly bool[] _wasDown = new bool[0x100];

        private static volatile bool _running;

        public static void Start()
        {
            if (_running) return; // 幂等

            DisableQuickEdit();

            _running = true;
            new Thread(Listen) { IsBackground = true }.Start();
        }

        public static void Stop()
        {
            _running = false;
            RestoreInputMode();
        }

        // 主循环每轮把积压的按键取空。取不到返回 false。
        public static bool TryRead(out KeyEvent e)
        {
            return _queue.TryDequeue(out e);
        }

        private static void Listen()
        {
            while (_running)
            {
                // 后台线程上的未捕获异常会终止整个进程（即游戏）。监听循环绝不能死。
                try { Poll(); }
                catch { }

                Thread.Sleep(PollMs);
            }
        }

        private static void Poll()
        {
            // 修饰键每轮取一次：同一次按下配到的是同一轮的 Ctrl 状态。
            // 这里只读状态、不入队，Ctrl 自身不作为一个事件上报。
            bool ctrl = Down(VkControl);

            foreach (int vk in Watched)
            {
                bool down = Down(vk);

                // GetAsyncKeyState 是电平式的，长按会持续为 true，只有 false→true 算一次按下
                if (down && !_wasDown[vk])
                    _queue.Enqueue(new KeyEvent { VirtualKey = vk, Ctrl = ctrl });

                _wasDown[vk] = down;
            }
        }

        // 只看最高位（当前是否按下）。最低位是"上次调用后按过"的粘滞标志，
        // 但它会被其它进程的 GetAsyncKeyState 调用重置，不可靠，故不用。
        private static bool Down(int vk)
        {
            return (GetAsyncKeyState(vk) & 0x8000) != 0;
        }

        // ---- 控制台输入模式 ----
        //
        // 与按键判断无关，但必须做：QuickEdit 打开时，在控制台里拖动鼠标选择文本会让
        // 后续的 WriteConsole **阻塞**，界面直接卡死。本程序持续重绘，拖选极易触发。
        // 代价是不能再用鼠标选文字复制。
        //
        // 退出时按保存的原值还原，不把用户的控制台改坏。

        private const int StdInputHandle = -10;
        private const uint EnableProcessedInput = 0x0001; // 保留 Ctrl+C 的默认语义
        private const uint EnableExtendedFlags = 0x0080;  // 必须置位，QuickEdit 的改动才生效

        private static IntPtr _input;
        private static uint _savedMode;

        private static void DisableQuickEdit()
        {
            _input = GetStdHandle(StdInputHandle);

            // 只留"处理输入 + 扩展标志"：行输入 / 回显 / 鼠标 / QuickEdit 全关。
            // 取不到模式说明标准输入不是控制台（被重定向），此时无处可设，跳过即可。
            if (GetConsoleMode(_input, out _savedMode))
                SetConsoleMode(_input, EnableProcessedInput | EnableExtendedFlags);
            else
                _input = IntPtr.Zero;
        }

        private static void RestoreInputMode()
        {
            if (_input != IntPtr.Zero) SetConsoleMode(_input, _savedMode);
        }

        [DllImport("user32.dll")]
        private static extern short GetAsyncKeyState(int vKey);

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern IntPtr GetStdHandle(int stdHandle);

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool GetConsoleMode(IntPtr handle, out uint mode);

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool SetConsoleMode(IntPtr handle, uint mode);
    }
}
