package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// go run main.go run <cmd> <args>
func main() {
	// os.Args 是一个 []string，保存程序启动时的命令行参数。
	// os.Args[0] 是程序的执行路径或名称，os.Args[1:] 是传入的参数。
	// 例如：go run main.go run /bin/bash
	// os.Args[1] = "run"，os.Args[2] = "/bin/bash"。
	switch os.Args[1] {
	case "run":
		run()
	case "child":
		child()
	default:
		panic("help")
	}
}

func run() {
	fmt.Printf("Running %v \n", os.Args[2:])

	// /proc/self 是 Linux 提供的特殊符号链接，指向当前访问进程的 /proc/<pid> 目录。
	// /proc/<pid>/exe 是符号链接，指向对应进程正在执行的可执行文件。
	// append 将 child 放在原有参数之前，最外层的 ... 将切片展开为可变参数。
	// 例如执行 go run main.go run /bin/bash：
	// go run 会先将 main.go 编译为临时可执行文件，再启动该程序。
	// 因此这里构造的命令相当于 /tmp/go-build123456/b001/exe/main child /bin/bash。
	// exec.Command 只构造命令，并不会立即启动子进程。
	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, os.Args[2:]...)...)

	// 将当前进程的标准输入、标准输出和标准错误输出连接给子进程。
	// Linux 约定文件描述符 0、1、2 分别对应 stdin、stdout、stderr。
	// 使子进程能够接收终端输入，并将输出直接显示在当前终端。
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	// SysProcAttr 用于配置子进程的 Linux 系统级属性。
	// Cloneflags：控制子进程创建时的行为，包括资源共享和 Namespace 创建等。
	// CLONE_NEWUTS：创建新的 UTS Namespace，隔离 hostname 等信息。
	// CLONE_NEWPID：创建新的 PID Namespace，隔离进程编号空间。
	// CLONE_NEWNS：创建新的 Mount Namespace，隔离文件系统挂载视图。
	// Unshareflags：指定子进程执行目标程序前，需要解除共享的资源或 Namespace。
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:   syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS,
		Unshareflags: syscall.CLONE_NEWNS,
	}

	must(cmd.Run())
}

func child() {
	fmt.Printf("Running %v \n", os.Args[2:])

	//  /tmp/go-build123456/b001/exe/main child /bin/bash 下
	// 这个命令是 /bin/bash
	cmd := exec.Command(os.Args[2], os.Args[3:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	// 修改当前 UTS Namespace 的 hostname 为 container。
	// 由于父进程创建子进程时指定了 CLONE_NEWUTS，
	// 因此不会修改宿主机所在 UTS Namespace 的 hostname。
	must(syscall.Sethostname([]byte("container")))

	// 将当前进程的文件系统根目录设置为指定的 RootFS。
	// 此后绝对路径 / 将对应宿主机的 /home/liz/ubuntufs。
	// chroot 不会自动修改当前工作目录，因此还需要调用 chdir。
	must(syscall.Chroot("/home/liz/ubuntufs"))
	must(os.Chdir("/"))

	// func Mount(source string, target string, fstype string, flags uintptr, data string) error
	// 
	// 参数	   当前值	   含义
	// source	"proc"	挂载源标识
	// target	"proc"	挂载目标目录
	// fstype	"proc"	文件系统类型为 procfs
	// flags	0	    不指定额外的挂载标志
	// data	    ""	    不提供额外的挂载选项
	// 
	// 在容器 RootFS 的 /proc 目录挂载新的 procfs。
	// procfs 是 Linux 提供的虚拟文件系统，用于暴露进程和内核信息。
	// 新挂载的 procfs 对应当前 PID Namespace，使容器能够查看自己的进程信息。
	must(syscall.Mount("proc", "proc", "proc", 0, ""))

	// 参数	     值	      含义
	// source	"thing"	   挂载源名称
	// target	"mytemp"   挂载目标目录
	// fstype	"tmpfs"	   指定文件系统类型
	// flags	0	       没有额外的挂载标志
	// data	    ""	       没有额外的挂载选项
	// 在容器的 /mytemp 目录挂载 tmpfs 临时文件系统。
	// thing 是挂载源名称，不对应实际的磁盘设备。
	// tmpfs 主要使用内存存储数据，卸载后数据会消失。
	// 挂载发生在当前 Mount Namespace 中。
	// thing 只是 tmpfs 的挂载源名称，不对应实际设备。
	must(syscall.Mount("thing", "mytemp", "tmpfs", 0, ""))
	
	cg()

	must(cmd.Run())

	// 卸载之前挂载的文件系统。
	// Unmount 接收挂载点路径，而不是 Mount 的 source。
	must(syscall.Unmount("proc", 0))
	must(syscall.Unmount("thing", 0))
}

func cg() {
	// Cgroups 目录，是内核提供的资源控制机制，文件系统只是供用户空间操作它的接口。
	// 这个是 Cgroups v1 目录
	// 依赖 RootFS 中已正确挂载 Cgroups v1 的 pids Controller。
	cgroups := "/sys/fs/cgroup/"
	pids := filepath.Join(cgroups, "pids")
	// 创建目录 /sys/fs/cgroup/pids/liz， 并设置目录权限为 0755。
	os.MkdirAll(filepath.Join(pids, "liz"), 0755)

    // /sys/fs/cgroup/pids/liz/pids.max 写入 20
    // pids.max 是 Cgroups v1 提供的任务数量限制接口。
    // 向其中写入 20，表示该 Cgroup 最多允许存在 20 个任务（包括线程）。
	must(ioutil.WriteFile(filepath.Join(pids, "liz/pids.max"), []byte("20"), 0700))

	// Removes the new cgroup in place after the container exits
	// 启用 Cgroups v1 的释放通知机制。
	// 当该 Cgroup 中没有进程和子 Cgroup 时，可触发 release_agent。
	// 是否自动删除目录，取决于系统是否配置了相应的清理程序。
	must(ioutil.WriteFile(filepath.Join(pids, "liz/notify_on_release"), []byte("1"), 0700))

	//  /sys/fs/cgroup/pids/liz/cgroup.procs 用于管理 Cgroup 中的进程成员关系。
	// 读取 cgroup.procs：可以查看属于该 Cgroup 的进程 PID。
	// 向 cgroup.procs 写入 PID：请求内核将对应进程迁移到该 Cgroup。
	// 子进程通常继承父进程的 Cgroup，因此之后启动的 Bash 也会受到 pids.max 限制。
	must(ioutil.WriteFile(filepath.Join(pids, "liz/cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0700))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
