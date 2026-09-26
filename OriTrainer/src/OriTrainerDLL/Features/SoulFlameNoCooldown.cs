using System.Threading;

namespace OriTrainerDLL.Features
{
    // 灵魂链接无需冷却：每 10ms 调用游戏自己的 FillSoulFlameBar() 把冷却清零。
    // 实现同终极版：FillSoulFlameBar() 只写 m_cooldownRemaining = 0，无副作用。
    public static class SoulFlameNoCooldown
    {
        private const int IntervalMs = 10;

        private static Timer _timer;

        public static void Start()
        {
            if (_timer != null) return; // 幂等：重复 Start 不重复起定时器

            _timer = new Timer(Clear, null, 0, IntervalMs);
        }

        public static void Stop()
        {
            if (_timer == null) return;

            _timer.Dispose();
            _timer = null;
        }

        private static void Clear(object state)
        {
            // 定时器回调里的未捕获异常会终止整个进程（即游戏），必须自己兜住
            try
            {
                Game.Characters.Sein.SoulFlame.FillSoulFlameBar();
            }
            catch { }
        }
    }
}
