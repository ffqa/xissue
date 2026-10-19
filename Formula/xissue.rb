class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.7"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.7/xissue-v0.2.7-darwin-arm64.tar.gz"
      sha256 "9503fa04a37aad4fceccd3ca1b2845327ae368b8ba31321c62b63ff753aff458"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.7/xissue-v0.2.7-darwin-amd64.tar.gz"
      sha256 "0921ed374aa9e1dc421072367d174a6fd53232341193c74b6898d515f4df2aaa"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.7/xissue-v0.2.7-linux-arm64.tar.gz"
      sha256 "4d926acefa944a8bdab924c0b99c990b897babeaf1cd9e5b6d2a024fd949783d"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.7/xissue-v0.2.7-linux-amd64.tar.gz"
      sha256 "5a7174b7716ca0779855f2b2499521736bf86ac696f8a1a76e32e9d58b735d63"
    end
  end

  def install
    bin.install "xissue"
    (pkgshare/"scripts").install Dir["scripts/*"] if Dir.exist?("scripts")
    (pkgshare/"deploy").install Dir["deploy/*"] if Dir.exist?("deploy")
  end

  service do
    run [opt_bin/"xissue"]
    keep_alive true
    working_dir Dir.home
    log_path var/"log/xissue.log"
    error_log_path var/"log/xissue.log"
  end

  def caveats
    <<~EOS
      启动后台常驻服务方式（二选一）：
      1. Homebrew 服务管理:
         brew services start ffqa/tap/xissue
      2. xIssue 自管服务 (推荐，开机自启):
         xissue service install
    EOS
  end

  test do
    assert_match "xissue", shell_output("#{bin}/xissue version")
  end
end
