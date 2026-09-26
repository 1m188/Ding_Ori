using System;
using System.Threading;

namespace OriTrainerDLL.Features
{
    // 重置时间：把游玩计时器归零，暂停界面显示 0:00:00。
    // 实现同终极版：GameTimer.Instance/CurrentTime/Reset() 全是 public，直接调官方方法。
    // 停止不还原：CurrentTime 是存档字段，开启期间存过档 0 就已落盘。
    public static class ResetTime
    {
        private const int IntervalMs = 10;

        private static Timer _timer;

        public static void Start()
        {
            if (_timer != null) return; // 幂等：重复 Start 不重复起定时器

            _timer = new Timer(Tick, null, 0, IntervalMs);
        }

        public static void Stop()
        {
            if (_timer == null) return;

            _timer.Dispose();
            _timer = null;
        }

        private static void Tick(object state)
        {
            // 定时器回调里的未捕获异常会终止整个进程（即游戏），必须自己兜住
            try
            {
                // 主菜单/读档过程中该单例可能尚未建立；每次都重新读静态字段：
                // 换场景/读档会重建 GameTimer。
                GameTimer timer = GameTimer.Instance;
                if (timer == null) return;

                timer.Reset(); // 官方方法，等价于 CurrentTime = 0f
            }
            catch { }
        }
    }
}
