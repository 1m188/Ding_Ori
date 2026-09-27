using System;

namespace OriTrainerDLL.Features
{
    // 重置时间：把游玩计时器归零，暂停界面显示 0:00:00。
    // 实现同终极版：GameTimer.Instance/CurrentTime/Reset() 全是 public，直接调官方方法。
    // 停止不还原：CurrentTime 是存档字段，开启期间存过档 0 就已落盘。
    //
    // 挂游戏自己的每帧回调 OnGameFixedUpdate（原版旧 Mono 的 System.Threading.Timer
    // 不可靠，回调不触发）。
    public static class ResetTime
    {
        private static Action _hook; // 保留引用以便 Stop 时注销

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            // Scheduler 由 GameController 持有，而 GameController.Awake 是单例守卫
            // （Instance 已存在则 Destroy 自身），所以该回调在整个进程内稳定可用。
            GameScheduler scheduler = Game.Events.Scheduler ?? throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            _hook = OnGameFixedUpdate;
            scheduler.OnGameFixedUpdate.Add(_hook);
        }

        public static void Stop()
        {
            if (_hook == null) return;

            Game.Events.Scheduler.OnGameFixedUpdate.Remove(_hook);
            _hook = null;

            // 刻意不还原 CurrentTime：见文件头"停止不还原"
        }

        // 由游戏主线程每个 FixedUpdate 调用
        private static void OnGameFixedUpdate()
        {
            // 游戏回调里抛出的异常会顺着 GameController.FixedUpdate 冒到 Unity，
            // 后果不可预期，必须自己兜住
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