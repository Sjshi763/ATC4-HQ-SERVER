FROM ubuntu:24.04
# 安装编译环境
RUN apt-get update && apt-get install -y g++ make
# 拷贝源码到容器
WORKDIR /app
COPY . .
# 编译你的程序
RUN g++ -o server mian.cpp
# 暴露端口
EXPOSE 10086
# 启动服务
CMD ["./server"]