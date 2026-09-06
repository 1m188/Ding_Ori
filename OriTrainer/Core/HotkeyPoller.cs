using System;
using System.Threading;

namespace OriTrainer.Core
{
    /// <summary>
    /// 全局热键轮询：数字键 1-9/0（大键盘与小键盘均可）与 HOME。
    /// 有意不用 RegisterHotKey——那会系统级吞掉数字键，影响用户在
    /// 浏览器/聊天窗口的正常输入；GetAsyncKeyState 轮询 + 边沿检测
    /// 是风灵月影系修改器的同款做法。
    /// </summary>
    internal sealed class HotkeyPoller : IDisposable
    {
        private static readonly int[] TopRow = { 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x30 };
        private static readonly int[] Numpad = { 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x60 };

        private readonly bool[] _prevDown = new bool[10];
        private bool _prevHome;
        private Thread _thread;
        private volatile bool _stop;

        /// <summary>参数 0..8 对应数字键 1..9，9 对应数字键 0。</summary>
        public event Action<int> NumberPressed;
        public event Action HomePressed;

        public void Start()
        {
            if (_thread != null) return;
            _thread = new Thread(PollLoop) { IsBackground = true, Name = "HotkeyPoller" };
            _thread.Start();
        }

        private void PollLoop()
        {
            while (!_stop)
            {
                for (int i = 0; i < 10; i++)
                {
                    bool down = (NativeMethods.GetAsyncKeyState(TopRow[i]) & 0x8000) != 0
                             || (NativeMethods.GetAsyncKeyState(Numpad[i]) & 0x8000) != 0;
                    if (down && !_prevDown[i])
                    {
                        var h = NumberPressed;
                        if (h != null) h(i);
                    }
                    _prevDown[i] = down;
                }

                bool home = (NativeMethods.GetAsyncKeyState(NativeMethods.VK_HOME) & 0x8000) != 0;
                if (home && !_prevHome)
                {
                    var h = HomePressed;
                    if (h != null) h();
                }
                _prevHome = home;

                Thread.Sleep(15);
            }
        }

        public void Dispose()
        {
            _stop = true;
        }
    }
}
